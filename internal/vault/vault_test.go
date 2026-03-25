package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewCreatesNotesDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(dir); err != nil {
		t.Fatal(err)
	}
	notesDir := filepath.Join(dir, "memory", "notes")
	if _, err := os.Stat(notesDir); os.IsNotExist(err) {
		t.Fatalf("vault.New() did not create memory/notes dir at %s", notesDir)
	}
}

func TestNotesDirReturnsCorrectPath(t *testing.T) {
	dir := t.TempDir()
	v, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(dir, "memory", "notes")
	if v.NotesDir() != expected {
		t.Fatalf("NotesDir() = %q, want %q", v.NotesDir(), expected)
	}
}
