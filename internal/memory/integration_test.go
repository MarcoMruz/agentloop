package memory

import (
	"strings"
	"testing"
)

// TestThreadIsolation_FullRoundTrip verifies that two Slack threads do not
// bleed context into each other.
func TestThreadIsolation_FullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	thread1 := "C111:t1"
	thread2 := "C222:t2"

	// Thread 1 messages
	eng.RecordInteraction("marco", "debug the login handler", "Found nil pointer in auth.go.", nil, thread1)
	eng.RecordInteraction("marco", "fix the nil pointer", "Applied fix, tests pass.", nil, thread1)

	// Thread 2 messages — completely unrelated
	eng.RecordInteraction("marco", "deploy to staging", "Deployed v1.2.3.", nil, thread2)

	// Thread 1 context must contain its own history only
	ctx1, err := eng.GetContextForUserAndConversationContext("marco", thread1)
	if err != nil {
		t.Fatalf("thread1 context error: %v", err)
	}
	if !strings.Contains(ctx1, "login handler") {
		t.Error("thread1 context missing 'login handler'")
	}
	if strings.Contains(ctx1, "deploy to staging") {
		t.Error("ISOLATION FAIL: thread1 context contains thread2 entry 'deploy to staging'")
	}

	// Thread 2 context must contain only its own history
	ctx2, err := eng.GetContextForUserAndConversationContext("marco", thread2)
	if err != nil {
		t.Fatalf("thread2 context error: %v", err)
	}
	if !strings.Contains(ctx2, "deploy to staging") {
		t.Error("thread2 context missing 'deploy to staging'")
	}
	if strings.Contains(ctx2, "login handler") {
		t.Error("ISOLATION FAIL: thread2 context contains thread1 entry 'login handler'")
	}

	// CLI session with no contextID gets global behavior (no crash)
	ctxGlobal, err := eng.GetContextForUserAndConversationContext("marco", "")
	if err != nil {
		t.Fatalf("global context error: %v", err)
	}
	_ = ctxGlobal // just verify no panic/error
}

// TestThreadIsolation_NewThread_GetsOnlyProfile verifies that a brand-new thread
// gets only the user profile, not history from unrelated threads.
func TestThreadIsolation_NewThread_GetsOnlyProfile(t *testing.T) {
	dir := t.TempDir()
	eng := NewEngine(dir, 4000, "rolling", 30)

	// Existing thread has history
	eng.RecordInteraction("marco", "deploy to prod", "Done.", nil, "C111:existing")

	// Totally new thread: no history yet
	ctx, err := eng.GetContextForUserAndConversationContext("marco", "C222:brand-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not contain history from the other thread
	if strings.Contains(ctx, "deploy to prod") {
		t.Error("ISOLATION FAIL: new thread context contains old thread history")
	}
}
