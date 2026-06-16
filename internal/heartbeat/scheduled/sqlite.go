package scheduled

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteTaskStore persists ScheduledTasks in per-user SQLite databases.
// Lazy DB open: each user's DB is opened on first access.
type SQLiteTaskStore struct {
	mu   sync.Mutex
	dir  string
	dbs  map[string]*sql.DB
}

// NewSQLiteTaskStore creates a SQLiteTaskStore rooted at tasksDir.
func NewSQLiteTaskStore(tasksDir string) (*SQLiteTaskStore, error) {
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return nil, fmt.Errorf("tasks dir: %w", err)
	}
	return &SQLiteTaskStore{
		dir: tasksDir,
		dbs: make(map[string]*sql.DB),
	}, nil
}

// db lazily opens the per-user database.
func (s *SQLiteTaskStore) db(userID string) (*sql.DB, error) {
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

// migrate runs the schema creation on the database (idempotent).
func (s *SQLiteTaskStore) migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			name        TEXT NOT NULL,
			schedule    TEXT NOT NULL,
			skill_path  TEXT NOT NULL,
			prompt      TEXT NOT NULL DEFAULT '',
			enabled     BOOLEAN NOT NULL DEFAULT 1,
			created_at  TEXT NOT NULL,
			next_run_at TEXT NOT NULL,
			last_run_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_user ON scheduled_tasks(user_id);
		CREATE INDEX IF NOT EXISTS idx_tasks_next_run ON scheduled_tasks(next_run_at);
	`)
	if err != nil {
		return fmt.Errorf("migrate scheduled_tasks table: %w", err)
	}
	return nil
}

// Add inserts a new task and returns its ID.
func (s *SQLiteTaskStore) Add(task ScheduledTask) (string, error) {
	db, err := s.db(task.UserID)
	if err != nil {
		return "", err
	}

	if task.ID == "" {
		task.ID = "stask-" + uuid.New().String()[:8]
	}
	now := time.Now()
	task.CreatedAt = now
	if task.Enabled && task.NextRunAt.IsZero() {
		task.NextRunAt = now
	}

	_, err = db.Exec(`
		INSERT INTO scheduled_tasks 
		(id, user_id, name, schedule, skill_path, prompt, enabled, created_at, next_run_at, last_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.ID, task.UserID, task.Name, task.Schedule, task.SkillPath, task.Prompt, 
		task.Enabled, task.CreatedAt.Format(time.RFC3339), task.NextRunAt.Format(time.RFC3339),
		formatTime(task.LastRunAt))
	if err != nil {
		return "", fmt.Errorf("insert task: %w", err)
	}

	return task.ID, nil
}

// Get retrieves a task by ID.
func (s *SQLiteTaskStore) Get(id string) (*ScheduledTask, error) {
	// Since we don't know the userID, we need to search across all open DBs
	// In production, callers should pass userID. For now, return not found.
	return nil, fmt.Errorf("Get without userID not implemented; use GetByID with userID")
}

// GetByID retrieves a task by ID, given the userID.
func (s *SQLiteTaskStore) GetByID(userID, id string) (*ScheduledTask, error) {
	db, err := s.db(userID)
	if err != nil {
		return nil, err
	}

	var task ScheduledTask
	var createdAtStr, nextRunAtStr, lastRunAtStr string
	err = db.QueryRow(`
		SELECT id, user_id, name, schedule, skill_path, prompt, enabled, created_at, next_run_at, last_run_at
		FROM scheduled_tasks WHERE id = ? AND user_id = ?
	`, id, userID).Scan(
		&task.ID, &task.UserID, &task.Name, &task.Schedule, &task.SkillPath, &task.Prompt,
		&task.Enabled, &createdAtStr, &nextRunAtStr, &lastRunAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query task: %w", err)
	}

	// Parse time fields
	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		task.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, nextRunAtStr); err == nil {
		task.NextRunAt = t
	}
	if lastRunAtStr != "" {
		if t, err := time.Parse(time.RFC3339, lastRunAtStr); err == nil {
			task.LastRunAt = t
		}
	}

	return &task, nil
}

// List returns all tasks for a user, newest first.
func (s *SQLiteTaskStore) List(userID string) ([]ScheduledTask, error) {
	db, err := s.db(userID)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT id, user_id, name, schedule, skill_path, prompt, enabled, created_at, next_run_at, last_run_at
		FROM scheduled_tasks WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		var task ScheduledTask
		var createdAtStr, nextRunAtStr, lastRunAtStr string
		err := rows.Scan(
			&task.ID, &task.UserID, &task.Name, &task.Schedule, &task.SkillPath, &task.Prompt,
			&task.Enabled, &createdAtStr, &nextRunAtStr, &lastRunAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		// Parse time fields
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			task.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, nextRunAtStr); err == nil {
			task.NextRunAt = t
		}
		if lastRunAtStr != "" {
			if t, err := time.Parse(time.RFC3339, lastRunAtStr); err == nil {
				task.LastRunAt = t
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// ListDue returns tasks that are due (NextRunAt <= now AND Enabled=true), oldest first.
func (s *SQLiteTaskStore) ListDue(userID string, now time.Time) ([]ScheduledTask, error) {
	db, err := s.db(userID)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT id, user_id, name, schedule, skill_path, prompt, enabled, created_at, next_run_at, last_run_at
		FROM scheduled_tasks 
		WHERE user_id = ? AND enabled = 1 AND next_run_at <= ?
		ORDER BY next_run_at ASC
	`, userID, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("query due tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		var task ScheduledTask
		var createdAtStr, nextRunAtStr, lastRunAtStr string
		err := rows.Scan(
			&task.ID, &task.UserID, &task.Name, &task.Schedule, &task.SkillPath, &task.Prompt,
			&task.Enabled, &createdAtStr, &nextRunAtStr, &lastRunAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scan due task: %w", err)
		}
		// Parse time fields
		if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			task.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339, nextRunAtStr); err == nil {
			task.NextRunAt = t
		}
		if lastRunAtStr != "" {
			if t, err := time.Parse(time.RFC3339, lastRunAtStr); err == nil {
				task.LastRunAt = t
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// UpdateLastRun updates the LastRunAt timestamp and computes the next run time.
func (s *SQLiteTaskStore) UpdateLastRun(id string, lastRunAt time.Time, nextSchedule string) error {
	// Caller must ensure userID is known; for now, search across open DBs
	// Better: require userID as parameter
	return fmt.Errorf("UpdateLastRun without userID not implemented; use UpdateLastRunByID with userID")
}

// UpdateLastRunByID updates last run and next run time, given the userID.
func (s *SQLiteTaskStore) UpdateLastRunByID(userID, id string, lastRunAt time.Time, nextSchedule string) error {
	db, err := s.db(userID)
	if err != nil {
		return err
	}

	nextRun := computeNextRun(lastRunAt, nextSchedule)
	_, err = db.Exec(`
		UPDATE scheduled_tasks SET last_run_at = ?, next_run_at = ?
		WHERE id = ? AND user_id = ?
	`, lastRunAt.Format(time.RFC3339), nextRun.Format(time.RFC3339), id, userID)
	if err != nil {
		return fmt.Errorf("update last run: %w", err)
	}
	return nil
}

// Delete removes a task.
func (s *SQLiteTaskStore) Delete(id string) error {
	return fmt.Errorf("Delete without userID not implemented; use DeleteByID with userID")
}

// DeleteByID removes a task, given the userID.
func (s *SQLiteTaskStore) DeleteByID(userID, id string) error {
	db, err := s.db(userID)
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM scheduled_tasks WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

// Disable sets Enabled to false.
func (s *SQLiteTaskStore) Disable(id string) error {
	return fmt.Errorf("Disable without userID not implemented; use DisableByID with userID")
}

// DisableByID sets Enabled to false, given the userID.
func (s *SQLiteTaskStore) DisableByID(userID, id string) error {
	db, err := s.db(userID)
	if err != nil {
		return err
	}

	_, err = db.Exec(`UPDATE scheduled_tasks SET enabled = 0 WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("disable task: %w", err)
	}
	return nil
}

// formatTime converts a time.Time to RFC3339 format, or empty string if zero.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
