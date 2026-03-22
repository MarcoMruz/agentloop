package notes

import (
	"testing"
	"time"
)

// conformanceTests verifies any NoteStore implementation satisfies the contract.
func conformanceTests(t *testing.T, store NoteStore) {
	t.Helper()

	// Add and Get
	note := AtomicNote{
		UserID:      "u1",
		Content:     "user prefers Go over Python",
		Keywords:    []string{"go", "python", "preference"},
		Tags:        []string{"language", "preference"},
		Description: "language preference fact",
		Embedding:   []float32{0.1, 0.2, 0.3},
	}
	id, err := store.Add(note)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if id == "" {
		t.Fatal("Add returned empty id")
	}

	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != note.Content {
		t.Fatalf("Get content mismatch: %q != %q", got.Content, note.Content)
	}

	// Update
	got.Content = "user prefers Go over Python for backend"
	if err := store.Update(*got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, _ := store.Get(id)
	if updated.Content != "user prefers Go over Python for backend" {
		t.Fatal("Update did not persist")
	}

	// SearchByKeywords
	results, err := store.SearchByKeywords("u1", []string{"go", "language"}, 10)
	if err != nil {
		t.Fatalf("SearchByKeywords: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchByKeywords returned no results")
	}

	// Delete
	if err := store.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(id)
	if err == nil {
		t.Fatal("Get after Delete should return error")
	}
}

func TestInMemoryNoteStoreConformance(t *testing.T) {
	conformanceTests(t, NewInMemoryNoteStore())
}

func TestAtomicNoteTimestamps(t *testing.T) {
	store := NewInMemoryNoteStore()
	id, err := store.Add(AtomicNote{UserID: "u1", Content: "test"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(id)
	if got.CreatedAt.IsZero() {
		t.Fatal("Add should set CreatedAt")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("Add should set UpdatedAt")
	}
	before := got.UpdatedAt
	time.Sleep(time.Millisecond)
	got.Content = "updated"
	store.Update(*got)
	got2, _ := store.Get(id)
	if !got2.UpdatedAt.After(before) {
		t.Fatal("Update should advance UpdatedAt")
	}
}

func TestListByUser(t *testing.T) {
	store := NewInMemoryNoteStore()
	store.Add(AtomicNote{UserID: "u1", Content: "a"})
	store.Add(AtomicNote{UserID: "u1", Content: "b"})
	store.Add(AtomicNote{UserID: "u2", Content: "c"})

	u1Notes, err := store.ListByUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(u1Notes) != 2 {
		t.Fatalf("expected 2 notes for u1, got %d", len(u1Notes))
	}
}

func TestSQLiteNoteStoreConformance(t *testing.T) {
	store, err := NewSQLiteNoteStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewSQLiteNoteStore: %v", err)
	}
	defer store.Close()
	conformanceTests(t, store)
}

func TestSQLiteNoteStoreVectorSearch(t *testing.T) {
	store, err := NewSQLiteNoteStore(t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	testNotes := []AtomicNote{
		{UserID: "u1", Content: "prefers Go", Keywords: []string{"go"}, Embedding: []float32{1.0, 0.0, 0.0}},
		{UserID: "u1", Content: "prefers Python", Keywords: []string{"python"}, Embedding: []float32{0.0, 1.0, 0.0}},
		{UserID: "u1", Content: "uses Docker", Keywords: []string{"docker"}, Embedding: []float32{0.0, 0.0, 1.0}},
	}
	for _, n := range testNotes {
		if _, err := store.Add(n); err != nil {
			t.Fatal(err)
		}
	}

	results, err := store.SearchByVector("u1", []float32{0.9, 0.1, 0.0}, 1)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "prefers Go" {
		t.Fatalf("expected 'prefers Go', got %q", results[0].Content)
	}
}

func TestSQLiteNoteStoreNoEmbeddingSkipped(t *testing.T) {
	store, err := NewSQLiteNoteStore(t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	id, err := store.Add(AtomicNote{UserID: "u1", Content: "no embedding note", Keywords: []string{"test"}})
	if err != nil {
		t.Fatalf("Add without embedding: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != "no embedding note" {
		t.Fatalf("unexpected content: %q", got.Content)
	}
}
