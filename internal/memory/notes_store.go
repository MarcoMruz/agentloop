package memory

import (
	_ "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// AtomicNotesStore will provide SQLite-backed persistent notes storage with vector similarity search.
// This is a placeholder for the atomic notes store implementation that will use:
// - github.com/mattn/go-sqlite3 for SQLite database operations
// - github.com/asg017/sqlite-vec-go-bindings for vector similarity search capabilities
type AtomicNotesStore struct {
	// TODO: Implement atomic notes store with SQLite backend
}
