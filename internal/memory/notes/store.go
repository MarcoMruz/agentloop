package notes

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AtomicNote is a self-contained unit of user knowledge (Zettelkasten-style).
// One idea per note. Stored with keywords, tags, and optional vector embedding
// for dual-mode retrieval (keyword Jaccard + cosine similarity).
type AtomicNote struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Content     string    `json:"content"`
	Keywords    []string  `json:"keywords"`
	Tags        []string  `json:"tags"`
	Description string    `json:"description"`
	Embedding   []float32 `json:"embedding,omitempty"`
	Connections []string  `json:"connections"` // IDs of related notes
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NoteStore defines the note storage interface.
// IMPORTANT: For test purposes only. Production code must use *SQLiteNoteStore directly.
// InMemoryNoteStore (below) implements this interface for unit tests.
type NoteStore interface {
	Add(note AtomicNote) (id string, err error)
	Get(id string) (*AtomicNote, error)
	Update(note AtomicNote) error
	Delete(id string) error
	// SearchByKeywords returns up to topK notes matching any keyword (Jaccard).
	SearchByKeywords(userID string, keywords []string, topK int) ([]AtomicNote, error)
	// SearchByVector returns up to topK nearest notes by cosine similarity.
	// Returns empty slice (no error) when the store has no embeddings.
	SearchByVector(userID string, embedding []float32, topK int) ([]AtomicNote, error)
	// ListByUser returns all notes for a user, newest first.
	ListByUser(userID string) ([]AtomicNote, error)
}

// InMemoryNoteStore is used in tests. Not for production use.
type InMemoryNoteStore struct {
	mu    sync.RWMutex
	notes map[string]AtomicNote
}

func NewInMemoryNoteStore() *InMemoryNoteStore {
	return &InMemoryNoteStore{notes: make(map[string]AtomicNote)}
}

func (s *InMemoryNoteStore) Add(note AtomicNote) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	note.ID = "note-" + uuid.New().String()[:8]
	now := time.Now()
	note.CreatedAt = now
	note.UpdatedAt = now
	if note.Connections == nil {
		note.Connections = []string{}
	}
	s.notes[note.ID] = note
	return note.ID, nil
}

func (s *InMemoryNoteStore) Get(id string) (*AtomicNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notes[id]
	if !ok {
		return nil, fmt.Errorf("note %q not found", id)
	}
	return &n, nil
}

func (s *InMemoryNoteStore) Update(note AtomicNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[note.ID]; !ok {
		return fmt.Errorf("note %q not found", note.ID)
	}
	note.UpdatedAt = time.Now()
	s.notes[note.ID] = note
	return nil
}

func (s *InMemoryNoteStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[id]; !ok {
		return fmt.Errorf("note %q not found", id)
	}
	delete(s.notes, id)
	return nil
}

func (s *InMemoryNoteStore) SearchByKeywords(userID string, keywords []string, topK int) ([]AtomicNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	kw := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		kw[strings.ToLower(k)] = true
	}
	var results []AtomicNote
	for _, n := range s.notes {
		if n.UserID != userID {
			continue
		}
		for _, k := range n.Keywords {
			if kw[strings.ToLower(k)] {
				results = append(results, n)
				break
			}
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (s *InMemoryNoteStore) SearchByVector(_ string, _ []float32, _ int) ([]AtomicNote, error) {
	// In-memory store does not support vector search (used in tests only).
	return nil, nil
}

func (s *InMemoryNoteStore) ListByUser(userID string) ([]AtomicNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []AtomicNote
	for _, n := range s.notes {
		if n.UserID == userID {
			results = append(results, n)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	return results, nil
}
