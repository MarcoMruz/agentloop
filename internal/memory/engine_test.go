package memory

import (
	"slices"
	"strings"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/memory/notes"
)

func TestRecordInteraction_TagsContextID(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	contextID := "C123456:1234567890.000100"
	eng.RecordInteraction("marco", "fix auth", "Done.", []string{"bash"}, contextID)

	entries, err := eng.conversations.GetRecentIndexedByContext("marco", contextID, 10)
	if err != nil {
		t.Fatalf("GetRecentIndexedByContext failed: %v", err)
	}
	// RecordInteraction records two entries: user + assistant
	if len(entries) < 2 {
		t.Errorf("expected at least 2 indexed entries, got %d", len(entries))
	}
}

func TestRecordInteraction_EmptyContextID_GlobalBehavior(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	eng.RecordInteraction("marco", "fix auth", "Done.", []string{"bash"}, "")

	// With empty contextID, all entries are visible via GetRecentIndexed
	entries, err := eng.conversations.GetRecentIndexed("marco", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("expected at least 2 entries for empty context, got %d", len(entries))
	}
	// Verify empty contextID is stored (not some other value)
	for _, e := range entries {
		if e.ConversationContextID != "" {
			t.Errorf("expected empty ConversationContextID for CLI interaction, got %q", e.ConversationContextID)
		}
	}
}

func TestRecordInteraction_InvalidatesCacheForContext(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	// Seed cache
	contextID := "C123456:t1"
	eng.cache.Set("ctx:marco:"+contextID, "stale context", 300)

	eng.RecordInteraction("marco", "new msg", "reply", nil, contextID)

	// Cache for this context should be invalidated
	if cached := eng.cache.Get("ctx:marco:" + contextID); cached != "" {
		t.Error("expected cache to be invalidated after RecordInteraction")
	}
}

func TestGetContextForUserAndConversationContext_IsolatesThread(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	thread1 := "C111:t1"
	thread2 := "C222:t2"

	eng.RecordInteraction("marco", "fix auth", "Done.", nil, thread1)
	eng.RecordInteraction("marco", "deploy k8s", "Deployed.", nil, thread2)

	ctx1, err := eng.GetContextForUserAndConversationContext("marco", thread1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(ctx1, "fix auth") {
		t.Error("expected thread1 context to contain 'fix auth'")
	}
	if strings.Contains(ctx1, "deploy k8s") {
		t.Error("ISOLATION FAIL: thread1 context must NOT contain thread2 entry 'deploy k8s'")
	}
}

func TestGetContextForUserAndConversationContext_EmptyContextID_FallsBack(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	// Empty contextID → delegates to GetContextForUser
	_, err := eng.GetContextForUserAndConversationContext("marco", "")
	if err != nil {
		t.Fatalf("unexpected error with empty contextID: %v", err)
	}
}

func TestAddNoteLinksRelatedNotes(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)
	store := notes.NewInMemoryNoteStore()
	eng.SetNoteStore(store)

	// Add two pre-existing notes
	id1, _ := eng.AddNote(notes.AtomicNote{
		UserID:   "marco",
		Content:  "user prefers Go for backend services",
		Keywords: []string{"go", "backend"},
		Tags:     []string{"preference"},
	})
	id2, _ := eng.AddNote(notes.AtomicNote{
		UserID:   "marco",
		Content:  "user dislikes Java",
		Keywords: []string{"java", "backend"},
		Tags:     []string{"preference"},
	})

	// Add a new note that shares keywords with both
	id3, _ := eng.AddNote(notes.AtomicNote{
		UserID:   "marco",
		Content:  "user uses Go for all new backend projects",
		Keywords: []string{"go", "backend", "projects"},
		Tags:     []string{"preference"},
	})

	// id3 should be connected to id1 and id2
	n3, err := store.Get(id3)
	if err != nil {
		t.Fatalf("Get id3: %v", err)
	}
	hasID1 := slices.Contains(n3.Connections, id1)
	hasID2 := slices.Contains(n3.Connections, id2)
	if !hasID1 || !hasID2 {
		t.Errorf("expected id3 connections to include id1 and id2, got %v", n3.Connections)
	}

	// id1 should be back-linked to id3
	n1, _ := store.Get(id1)
	if !slices.Contains(n1.Connections, id3) {
		t.Errorf("expected id1 to be back-linked to id3, got %v", n1.Connections)
	}
}
