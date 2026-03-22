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
