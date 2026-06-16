package server

import (
	"testing"
	"time"

	"github.com/MarcoMruz/agentloop/internal/heartbeat/scheduled"
)

func TestExtractFeedbackPrefix(t *testing.T) {
	cases := []struct {
		input    string
		wantText string
		wantOK   bool
	}{
		// colon prefix variations
		{"feedback: the agent was wrong", "the agent was wrong", true},
		{"feedback:the agent was wrong", "the agent was wrong", true},
		{"FEEDBACK: uppercase", "uppercase", true},
		{"Feedback:mixed case", "mixed case", true},
		{"  feedback:  trimmed spaces  ", "trimmed spaces", true},

		// slash-command style
		{"/feedback this was bad", "this was bad", true},
		{"/FEEDBACK uppercase slash", "uppercase slash", true},
		{"  /feedback   leading spaces  ", "leading spaces", true},

		// non-feedback inputs — must not match
		{"fix the login bug", "", false},
		{"task: do something", "", false},
		{"refactor the auth module", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		gotText, gotOK := extractFeedbackPrefix(tc.input)
		if gotOK != tc.wantOK {
			t.Errorf("extractFeedbackPrefix(%q): ok=%v, want %v", tc.input, gotOK, tc.wantOK)
			continue
		}
		if gotOK && gotText != tc.wantText {
			t.Errorf("extractFeedbackPrefix(%q): text=%q, want %q", tc.input, gotText, tc.wantText)
		}
	}
}

func TestExtractSchedulePrefix(t *testing.T) {
	cases := []struct {
		input    string
		wantText string
		wantOK   bool
	}{
		// colon prefix variations
		{"schedule: daily backup at 9am", "daily backup at 9am", true},
		{"schedule:daily backup at 9am", "daily backup at 9am", true},
		{"SCHEDULE: uppercase", "uppercase", true},
		{"Schedule:mixed case", "mixed case", true},
		{"  schedule:  trimmed spaces  ", "trimmed spaces", true},

		// slash-command style
		{"/schedule every 2 hours", "every 2 hours", true},
		{"/SCHEDULE uppercase slash", "uppercase slash", true},
		{"  /schedule   leading spaces  ", "leading spaces", true},

		// non-schedule inputs — must not match
		{"run the daily job", "", false},
		{"task: do something", "", false},
		{"feedback: agent was wrong", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		gotText, gotOK := extractSchedulePrefix(tc.input)
		if gotOK != tc.wantOK {
			t.Errorf("extractSchedulePrefix(%q): ok=%v, want %v", tc.input, gotOK, tc.wantOK)
			continue
		}
		if gotOK && gotText != tc.wantText {
			t.Errorf("extractSchedulePrefix(%q): text=%q, want %q", tc.input, gotText, tc.wantText)
		}
	}
}

func TestScheduleListMethod(t *testing.T) {
	// Create an in-memory task store
	store := scheduled.NewInMemoryTaskStore()

	// Add a test task
	task := scheduled.ScheduledTask{
		ID:        "task-1",
		UserID:    "user-1",
		Name:      "daily backup",
		Schedule:  "0 9 * * *",
		Enabled:   true,
		CreatedAt: time.Now(),
		NextRunAt: time.Now().Add(24 * time.Hour),
	}
	store.Add(task)

	// Verify list retrieves the task
	tasks, err := store.List("user-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "daily backup" {
		t.Errorf("expected name 'daily backup', got %q", tasks[0].Name)
	}
}

func TestScheduleDeleteMethod(t *testing.T) {
	// Create an in-memory task store
	store := scheduled.NewInMemoryTaskStore()

	// Add a test task
	task := scheduled.ScheduledTask{
		ID:        "task-1",
		UserID:    "user-1",
		Name:      "daily backup",
		Schedule:  "0 9 * * *",
		Enabled:   true,
		CreatedAt: time.Now(),
		NextRunAt: time.Now().Add(24 * time.Hour),
	}
	store.Add(task)

	// Delete the task
	err := store.Delete("task-1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify task is gone
	tasks, _ := store.List("user-1")
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after delete, got %d", len(tasks))
	}
}
