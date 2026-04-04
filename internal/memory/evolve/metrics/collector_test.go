package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectorPersist(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 300, 10)

	outcome := TaskOutcome{
		SessionID:   "sess-123",
		UserID:      "marco",
		Timestamp:   time.Now(),
		FinalStatus: "done",
	}

	c.Record(outcome)

	dateStr := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, "metrics", "marco-"+dateStr+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected metrics file: %v", err)
	}
	if !strings.Contains(string(data), "sess-123") {
		t.Fatal("expected session ID in metrics file")
	}
}

func TestCollectorRateLimitingCooldown(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 60, 10)

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	bad := TaskOutcome{
		SessionID:   "sess-1",
		UserID:      "marco",
		Timestamp:   time.Now(),
		FinalStatus: "aborted",
		HITLDenials: 3,
	}

	c.Record(bad)
	// Give goroutine time to run
	time.Sleep(10 * time.Millisecond)
	if triggered != 1 {
		t.Fatalf("expected 1 trigger, got %d", triggered)
	}

	bad.SessionID = "sess-2"
	c.Record(bad)
	time.Sleep(10 * time.Millisecond)
	if triggered != 1 {
		t.Fatalf("expected still 1 trigger (cooldown), got %d", triggered)
	}
}

func TestCollectorRateLimitingDailyCap(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 0, 2)

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	bad := TaskOutcome{
		FinalStatus: "aborted",
		HITLDenials: 3,
		Timestamp:   time.Now(),
	}

	for i := 0; i < 5; i++ {
		bad.SessionID = string(rune('a' + i))
		c.Record(bad)
	}

	time.Sleep(50 * time.Millisecond)
	if triggered != 2 {
		t.Fatalf("expected 2 triggers (daily cap), got %d", triggered)
	}
}

func TestCollectorRecordFeedback(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 0, 10)

	// Pre-populate an outcome so findAndEnrichOutcome can find it.
	outcome := TaskOutcome{
		SessionID:   "sess-feedback",
		UserID:      "marco",
		Timestamp:   time.Now(),
		FinalStatus: "done",
	}
	c.Record(outcome)

	fb := UserFeedback{
		SessionID:    "sess-feedback",
		UserID:       "marco",
		Timestamp:    time.Now(),
		FeedbackText: "agent ignored my instructions",
	}
	c.RecordFeedback(fb)

	// Feedback file must exist.
	dateStr := time.Now().Format("2006-01-02")
	fbPath := filepath.Join(dir, "metrics", "marco-feedback-"+dateStr+".jsonl")
	data, err := os.ReadFile(fbPath)
	if err != nil {
		t.Fatalf("expected feedback file: %v", err)
	}
	if !strings.Contains(string(data), "sess-feedback") {
		t.Fatal("feedback file missing session_id")
	}
	if !strings.Contains(string(data), "agent ignored my instructions") {
		t.Fatal("feedback file missing feedback_text")
	}
}

func TestCollectorFeedbackTriggersEvolution(t *testing.T) {
	dir := t.TempDir()
	// High threshold so a normal good outcome would NOT trigger.
	c := NewCollector(dir, 0.99, 0, 10)

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	// Record a perfect outcome — should NOT trigger (score=1.0 < 0.99 is false).
	good := TaskOutcome{
		SessionID:   "sess-good",
		UserID:      "marco",
		Timestamp:   time.Now(),
		FinalStatus: "done",
	}
	c.Record(good)
	time.Sleep(20 * time.Millisecond)
	if triggered != 0 {
		t.Fatalf("expected 0 triggers for good outcome, got %d", triggered)
	}

	// Feedback always triggers regardless of score.
	fb := UserFeedback{
		SessionID:    "sess-good",
		UserID:       "marco",
		Timestamp:    time.Now(),
		FeedbackText: "wrong answer",
	}
	c.RecordFeedback(fb)
	time.Sleep(20 * time.Millisecond)
	if triggered != 1 {
		t.Fatalf("expected 1 trigger from feedback, got %d", triggered)
	}
}

func TestCollectorFeedbackRespectsRateLimit(t *testing.T) {
	dir := t.TempDir()
	// cooldown=60s, daily cap=2
	c := NewCollector(dir, 0.99, 60, 2)

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	sendFeedback := func(id string) {
		c.RecordFeedback(UserFeedback{
			SessionID:    id,
			UserID:       "marco",
			Timestamp:    time.Now(),
			FeedbackText: "bad",
		})
	}

	// First feedback — triggers (resets cooldown clock).
	sendFeedback("sess-1")
	time.Sleep(20 * time.Millisecond)
	if triggered != 1 {
		t.Fatalf("expected 1 trigger, got %d", triggered)
	}

	// Second feedback — blocked by cooldown (60 s not elapsed).
	sendFeedback("sess-2")
	time.Sleep(20 * time.Millisecond)
	if triggered != 1 {
		t.Fatalf("expected still 1 trigger (cooldown), got %d", triggered)
	}
}

func TestCollectorGoodScoreNoTrigger(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 0, 100)

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	good := TaskOutcome{
		SessionID:   "sess-ok",
		FinalStatus: "done",
		Timestamp:   time.Now(),
	}
	c.Record(good)

	time.Sleep(10 * time.Millisecond)
	if triggered != 0 {
		t.Fatalf("expected 0 triggers for good score, got %d", triggered)
	}
}
