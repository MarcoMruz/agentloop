package notes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	vec.Auto()
}

// SQLiteNoteStore persists AtomicNotes in per-user SQLite databases.
// When embeddingDim > 0, a vec0 virtual table is created for vector search.
type SQLiteNoteStore struct {
	mu           sync.Mutex
	dir          string
	embeddingDim int
	dbs          map[string]*sql.DB
}

// NewSQLiteNoteStore creates a SQLiteNoteStore rooted at notesDir.
// embeddingDim sets the vector dimension; use 0 to disable vector search.
func NewSQLiteNoteStore(notesDir string, embeddingDim int) (*SQLiteNoteStore, error) {
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		return nil, fmt.Errorf("notes dir: %w", err)
	}
	return &SQLiteNoteStore{
		dir:          notesDir,
		embeddingDim: embeddingDim,
		dbs:          make(map[string]*sql.DB),
	}, nil
}

func (s *SQLiteNoteStore) db(userID string) (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if db, ok := s.dbs[userID]; ok {
		return db, nil
	}
	path := filepath.Join(s.dir, userID+".db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open db for %q: %w", userID, err)
	}
	if err := s.migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	s.dbs[userID] = db
	return db, nil
}

func (s *SQLiteNoteStore) migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS notes (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			content     TEXT NOT NULL,
			keywords    TEXT NOT NULL DEFAULT '[]',
			tags        TEXT NOT NULL DEFAULT '[]',
			description TEXT NOT NULL DEFAULT '',
			connections TEXT NOT NULL DEFAULT '[]',
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_notes_user ON notes(user_id);
	`)
	if err != nil {
		return fmt.Errorf("migrate notes table: %w", err)
	}
	if s.embeddingDim > 0 {
		_, err = db.Exec(fmt.Sprintf(`
			CREATE VIRTUAL TABLE IF NOT EXISTS note_embeddings USING vec0(
				note_id TEXT PRIMARY KEY,
				embedding float[%d]
			);
		`, s.embeddingDim))
		if err != nil {
			return fmt.Errorf("migrate vec0 table: %w", err)
		}
	}
	return nil
}

// Add inserts a new note, assigns an ID, and returns it.
func (s *SQLiteNoteStore) Add(note AtomicNote) (string, error) {
	db, err := s.db(note.UserID)
	if err != nil {
		return "", err
	}
	note.ID = "note-" + uuid.New().String()[:8]
	now := time.Now().UTC()
	note.CreatedAt = now
	note.UpdatedAt = now
	if note.Connections == nil {
		note.Connections = []string{}
	}

	kw, _ := json.Marshal(note.Keywords)
	tags, _ := json.Marshal(note.Tags)
	conns, _ := json.Marshal(note.Connections)

	_, err = db.Exec(`
		INSERT INTO notes (id, user_id, content, keywords, tags, description, connections, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		note.ID, note.UserID, note.Content, string(kw), string(tags),
		note.Description, string(conns),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return "", fmt.Errorf("insert note: %w", err)
	}

	if s.embeddingDim > 0 && len(note.Embedding) == s.embeddingDim {
		embBytes, err := vec.SerializeFloat32(note.Embedding)
		if err != nil {
			return note.ID, fmt.Errorf("serialize embedding: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO note_embeddings (note_id, embedding) VALUES (?, ?)`, note.ID, embBytes); err != nil {
			return note.ID, fmt.Errorf("insert embedding: %w", err)
		}
	}
	return note.ID, nil
}

// Get retrieves a note by ID, searching across all open user databases.
func (s *SQLiteNoteStore) Get(id string) (*AtomicNote, error) {
	s.mu.Lock()
	dbs := make([]*sql.DB, 0, len(s.dbs))
	for _, db := range s.dbs {
		dbs = append(dbs, db)
	}
	s.mu.Unlock()

	for _, db := range dbs {
		note, err := scanNoteByID(db, id)
		if err == nil {
			return note, nil
		}
	}
	return nil, fmt.Errorf("note %q not found", id)
}

func scanNoteByID(db *sql.DB, id string) (*AtomicNote, error) {
	row := db.QueryRow(`
		SELECT id, user_id, content, keywords, tags, description, connections, created_at, updated_at
		FROM notes WHERE id = ?`, id)
	return scanNoteRow(row)
}

func scanNoteRow(row *sql.Row) (*AtomicNote, error) {
	var n AtomicNote
	var kw, tags, conns, createdAt, updatedAt string
	err := row.Scan(&n.ID, &n.UserID, &n.Content, &kw, &tags, &n.Description, &conns, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("not found")
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(kw), &n.Keywords)
	json.Unmarshal([]byte(tags), &n.Tags)
	json.Unmarshal([]byte(conns), &n.Connections)
	n.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &n, nil
}

// Update overwrites a note's mutable fields.
func (s *SQLiteNoteStore) Update(note AtomicNote) error {
	db, err := s.db(note.UserID)
	if err != nil {
		return err
	}
	note.UpdatedAt = time.Now().UTC()
	kw, _ := json.Marshal(note.Keywords)
	tags, _ := json.Marshal(note.Tags)
	conns, _ := json.Marshal(note.Connections)

	res, err := db.Exec(`
		UPDATE notes SET content=?, keywords=?, tags=?, description=?, connections=?, updated_at=?
		WHERE id=?`,
		note.Content, string(kw), string(tags), note.Description, string(conns),
		note.UpdatedAt.Format(time.RFC3339Nano), note.ID)
	if err != nil {
		return fmt.Errorf("update note: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("note %q not found", note.ID)
	}
	if s.embeddingDim > 0 && len(note.Embedding) == s.embeddingDim {
		embBytes, _ := vec.SerializeFloat32(note.Embedding)
		db.Exec(`INSERT OR REPLACE INTO note_embeddings (note_id, embedding) VALUES (?, ?)`, note.ID, embBytes)
	}
	return nil
}

// Delete removes a note by ID.
func (s *SQLiteNoteStore) Delete(id string) error {
	s.mu.Lock()
	dbs := make([]*sql.DB, 0, len(s.dbs))
	for _, db := range s.dbs {
		dbs = append(dbs, db)
	}
	s.mu.Unlock()

	for _, db := range dbs {
		res, err := db.Exec(`DELETE FROM notes WHERE id=?`, id)
		if err != nil {
			continue
		}
		rows, _ := res.RowsAffected()
		if rows > 0 {
			db.Exec(`DELETE FROM note_embeddings WHERE note_id=?`, id)
			return nil
		}
	}
	return fmt.Errorf("note %q not found", id)
}

// SearchByKeywords returns up to topK notes for userID that match any keyword.
// Matching is pushed to SQLite via json_each() so only matching rows are returned.
func (s *SQLiteNoteStore) SearchByKeywords(userID string, keywords []string, topK int) ([]AtomicNote, error) {
	if len(keywords) == 0 {
		return nil, nil
	}
	db, err := s.db(userID)
	if err != nil {
		return nil, err
	}

	placeholders := strings.Repeat("?,", len(keywords))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, 1+len(keywords)+1)
	args = append(args, userID)
	for _, k := range keywords {
		args = append(args, strings.ToLower(k))
	}
	args = append(args, topK)

	rows, err := db.Query(fmt.Sprintf(`
		SELECT DISTINCT n.id, n.user_id, n.content, n.keywords, n.tags, n.description, n.connections, n.created_at, n.updated_at
		FROM notes n, json_each(n.keywords) ke
		WHERE n.user_id = ?
		  AND lower(ke.value) IN (%s)
		ORDER BY n.created_at DESC
		LIMIT ?`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AtomicNote
	for rows.Next() {
		var n AtomicNote
		var kwJSON, tagsJSON, connsJSON, createdAt, updatedAt string
		if err := rows.Scan(&n.ID, &n.UserID, &n.Content, &kwJSON, &tagsJSON, &n.Description, &connsJSON, &createdAt, &updatedAt); err != nil {
			continue
		}
		json.Unmarshal([]byte(kwJSON), &n.Keywords)
		json.Unmarshal([]byte(tagsJSON), &n.Tags)
		json.Unmarshal([]byte(connsJSON), &n.Connections)
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		results = append(results, n)
	}
	return results, nil
}

// SearchByVector returns up to topK notes nearest to the given embedding.
// Returns nil (no error) when embeddingDim is 0 or embedding length mismatches.
func (s *SQLiteNoteStore) SearchByVector(userID string, embedding []float32, topK int) ([]AtomicNote, error) {
	if s.embeddingDim == 0 || len(embedding) != s.embeddingDim {
		return nil, nil
	}
	db, err := s.db(userID)
	if err != nil {
		return nil, err
	}
	embBytes, err := vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, fmt.Errorf("serialize query embedding: %w", err)
	}

	rows, err := db.Query(`
		SELECT n.id, n.user_id, n.content, n.keywords, n.tags, n.description, n.connections, n.created_at, n.updated_at
		FROM note_embeddings ve
		JOIN notes n ON n.id = ve.note_id
		WHERE ve.embedding MATCH ? AND k = ?
		ORDER BY distance`,
		embBytes, topK)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var results []AtomicNote
	for rows.Next() {
		var n AtomicNote
		var kwJSON, tagsJSON, connsJSON, createdAt, updatedAt string
		if err := rows.Scan(&n.ID, &n.UserID, &n.Content, &kwJSON, &tagsJSON, &n.Description, &connsJSON, &createdAt, &updatedAt); err != nil {
			continue
		}
		json.Unmarshal([]byte(kwJSON), &n.Keywords)
		json.Unmarshal([]byte(tagsJSON), &n.Tags)
		json.Unmarshal([]byte(connsJSON), &n.Connections)
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		results = append(results, n)
	}
	return results, nil
}

// ListByUser returns all notes for a user, newest first.
func (s *SQLiteNoteStore) ListByUser(userID string) ([]AtomicNote, error) {
	db, err := s.db(userID)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT id, user_id, content, keywords, tags, description, connections, created_at, updated_at
		FROM notes WHERE user_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AtomicNote
	for rows.Next() {
		var n AtomicNote
		var kwJSON, tagsJSON, connsJSON, createdAt, updatedAt string
		if err := rows.Scan(&n.ID, &n.UserID, &n.Content, &kwJSON, &tagsJSON, &n.Description, &connsJSON, &createdAt, &updatedAt); err != nil {
			continue
		}
		json.Unmarshal([]byte(kwJSON), &n.Keywords)
		json.Unmarshal([]byte(tagsJSON), &n.Tags)
		json.Unmarshal([]byte(connsJSON), &n.Connections)
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		results = append(results, n)
	}
	return results, nil
}

// Close closes all open user database connections.
func (s *SQLiteNoteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, db := range s.dbs {
		db.Close()
	}
	return nil
}
