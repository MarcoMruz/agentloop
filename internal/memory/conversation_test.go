package memory

import (
	"testing"
)

func TestAppend_TagsConversationContextID(t *testing.T) {
	dir := t.TempDir()
	store := NewConversationStore(dir, 30)

	contextID := "C123456:1234567890.000100"
	if err := store.Append("marco", "user", "fix auth", contextID); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	entries, err := store.GetRecentIndexed("marco", 10)
	if err != nil {
		t.Fatalf("GetRecentIndexed failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 entry")
	}
	if entries[0].ConversationContextID != contextID {
		t.Errorf("got ConversationContextID=%q, want %q", entries[0].ConversationContextID, contextID)
	}
}

func TestGetRecentIndexedByContext_FiltersCorrectly(t *testing.T) {
	dir := t.TempDir()
	store := NewConversationStore(dir, 30)

	_ = store.Append("marco", "user", "fix auth", "C111:t1")
	_ = store.Append("marco", "user", "deploy k8s", "C222:t2")
	_ = store.Append("marco", "user", "fix login", "C111:t1")

	entries, err := store.GetRecentIndexedByContext("marco", "C111:t1", 10)
	if err != nil {
		t.Fatalf("GetRecentIndexedByContext failed: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for C111:t1, got %d", len(entries))
	}
	for _, e := range entries {
		if e.ConversationContextID != "C111:t1" {
			t.Errorf("unexpected contextID %q in filtered results", e.ConversationContextID)
		}
	}
}

func TestGetRecentIndexedByContext_EmptyContextIDReturnsAll(t *testing.T) {
	dir := t.TempDir()
	store := NewConversationStore(dir, 30)

	_ = store.Append("marco", "user", "msg1", "C111:t1")
	_ = store.Append("marco", "user", "msg2", "C222:t2")

	// Empty contextID → falls through to GetRecentIndexed (all entries)
	entries, err := store.GetRecentIndexedByContext("marco", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for empty context, got %d", len(entries))
	}
}
