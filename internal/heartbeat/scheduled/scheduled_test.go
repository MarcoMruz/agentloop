package scheduled

import (
	"testing"
	"time"
)

// TestScheduledTaskCRUD tests basic CRUD operations on ScheduledTasks.
func TestScheduledTaskCRUD(t *testing.T) {
	store := NewInMemoryTaskStore()
	userID := "user1"

	// Create a task
	task := ScheduledTask{
		ID:        "stask-1",
		UserID:    userID,
		Name:      "Daily standup",
		Schedule:  "daily at 9am",
		SkillPath: "/path/to/skill",
		Prompt:    "Run the standup meeting",
		Enabled:   true,
		NextRunAt: time.Now(),
	}

	id, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if id != "stask-1" {
		t.Errorf("expected id stask-1, got %q", id)
	}

	// Get the task
	retrieved, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Name != "Daily standup" {
		t.Errorf("expected name 'Daily standup', got %q", retrieved.Name)
	}

	// List tasks
	tasks, err := store.List(userID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	// Delete the task
	err = store.Delete(id)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's deleted
	tasks, err = store.List(userID)
	if err != nil {
		t.Fatalf("List after delete failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

// TestListDueFiltering tests that ListDue returns only tasks that are enabled and due.
func TestListDueFiltering(t *testing.T) {
	store := NewInMemoryTaskStore()
	userID := "user1"
	now := time.Now()

	// Create tasks with different statuses
	tasks := []ScheduledTask{
		{
			ID:        "stask-past",
			UserID:    userID,
			Name:      "Past task",
			Schedule:  "daily",
			SkillPath: "/path1",
			Enabled:   true,
			NextRunAt: now.Add(-1 * time.Hour), // In the past
		},
		{
			ID:        "stask-future",
			UserID:    userID,
			Name:      "Future task",
			Schedule:  "daily",
			SkillPath: "/path2",
			Enabled:   true,
			NextRunAt: now.Add(24 * time.Hour), // In the future
		},
		{
			ID:        "stask-disabled",
			UserID:    userID,
			Name:      "Disabled task",
			Schedule:  "daily",
			SkillPath: "/path3",
			Enabled:   false,
			NextRunAt: now.Add(-1 * time.Hour), // Due but disabled
		},
	}

	for _, task := range tasks {
		_, err := store.Add(task)
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	// Query due tasks
	dueTasksList, err := store.ListDue(userID, now)
	if err != nil {
		t.Fatalf("ListDue failed: %v", err)
	}

	// Should only return the past task (enabled and due)
	if len(dueTasksList) != 1 {
		t.Errorf("expected 1 due task, got %d", len(dueTasksList))
	}
	if len(dueTasksList) > 0 && dueTasksList[0].Name != "Past task" {
		t.Errorf("expected due task to be 'Past task', got %q", dueTasksList[0].Name)
	}
}

// TestDisableTask tests disabling a task.
func TestDisableTask(t *testing.T) {
	store := NewInMemoryTaskStore()
	userID := "user1"

	task := ScheduledTask{
		ID:        "stask-1",
		UserID:    userID,
		Name:      "Test task",
		Schedule:  "daily",
		SkillPath: "/path",
		Enabled:   true,
		NextRunAt: time.Now(),
	}

	_, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Disable the task
	err = store.Disable("stask-1")
	if err != nil {
		t.Fatalf("Disable failed: %v", err)
	}

	// Verify it's disabled
	retrieved, err := store.Get("stask-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Enabled {
		t.Errorf("expected task to be disabled")
	}

	// Verify it doesn't appear in ListDue
	due, err := store.ListDue(userID, time.Now())
	if err != nil {
		t.Fatalf("ListDue failed: %v", err)
	}
	if len(due) > 0 {
		t.Errorf("expected no due tasks after disabling, got %d", len(due))
	}
}

// TestSQLiteTaskStoreCRUD tests SQLite CRUD operations.
func TestSQLiteTaskStoreCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore failed: %v", err)
	}

	userID := "user1"

	// Create a task
	task := ScheduledTask{
		UserID:    userID,
		Name:      "Daily standup",
		Schedule:  "daily at 9am",
		SkillPath: "/path/to/skill",
		Prompt:    "Run the standup meeting",
		Enabled:   true,
	}

	id, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if id == "" {
		t.Errorf("expected non-empty id")
	}

	// Get the task
	retrieved, err := store.GetByID(userID, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if retrieved.Name != "Daily standup" {
		t.Errorf("expected name 'Daily standup', got %q", retrieved.Name)
	}
	if !retrieved.CreatedAt.IsZero() {
		t.Logf("CreatedAt was set: %v", retrieved.CreatedAt)
	}

	// List tasks
	tasks, err := store.List(userID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}
}

// TestSQLiteListDueFiltering tests SQLite ListDue with proper filtering.
func TestSQLiteListDueFiltering(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore failed: %v", err)
	}

	userID := "user1"
	now := time.Now()

	// Create tasks with different statuses
	tasks := []ScheduledTask{
		{
			UserID:    userID,
			Name:      "Past task",
			Schedule:  "daily",
			SkillPath: "/path1",
			Enabled:   true,
			NextRunAt: now.Add(-1 * time.Hour), // In the past
		},
		{
			UserID:    userID,
			Name:      "Future task",
			Schedule:  "daily",
			SkillPath: "/path2",
			Enabled:   true,
			NextRunAt: now.Add(24 * time.Hour), // In the future
		},
		{
			UserID:    userID,
			Name:      "Disabled task",
			Schedule:  "daily",
			SkillPath: "/path3",
			Enabled:   false,
			NextRunAt: now.Add(-1 * time.Hour), // Due but disabled
		},
	}

	for _, task := range tasks {
		_, err := store.Add(task)
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	// Query due tasks
	dueTasksList, err := store.ListDue(userID, now)
	if err != nil {
		t.Fatalf("ListDue failed: %v", err)
	}

	// Should only return the past task (enabled and due)
	if len(dueTasksList) != 1 {
		t.Errorf("expected 1 due task, got %d", len(dueTasksList))
	}
	if len(dueTasksList) > 0 && dueTasksList[0].Name != "Past task" {
		t.Errorf("expected due task to be 'Past task', got %q", dueTasksList[0].Name)
	}
}

// TestSQLiteDisableTask tests disabling via SQLite.
func TestSQLiteDisableTask(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore failed: %v", err)
	}

	userID := "user1"

	task := ScheduledTask{
		UserID:    userID,
		Name:      "Test task",
		Schedule:  "daily",
		SkillPath: "/path",
		Enabled:   true,
		NextRunAt: time.Now(),
	}

	id, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Disable the task
	err = store.DisableByID(userID, id)
	if err != nil {
		t.Fatalf("DisableByID failed: %v", err)
	}

	// Verify it's disabled
	retrieved, err := store.GetByID(userID, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if retrieved.Enabled {
		t.Errorf("expected task to be disabled")
	}

	// Verify it doesn't appear in ListDue
	due, err := store.ListDue(userID, time.Now())
	if err != nil {
		t.Fatalf("ListDue failed: %v", err)
	}
	if len(due) > 0 {
		t.Errorf("expected no due tasks after disabling, got %d", len(due))
	}
}

// TestSQLiteUpdateLastRun tests updating the last run time and computing next run.
func TestSQLiteUpdateLastRun(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore failed: %v", err)
	}

	userID := "user1"

	task := ScheduledTask{
		UserID:    userID,
		Name:      "Test task",
		Schedule:  "daily",
		SkillPath: "/path",
		Enabled:   true,
		NextRunAt: time.Now().Add(-1 * time.Hour),
	}

	id, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Update last run time
	lastRunTime := time.Now()
	err = store.UpdateLastRunByID(userID, id, lastRunTime, "daily")
	if err != nil {
		t.Fatalf("UpdateLastRunByID failed: %v", err)
	}

	// Verify the update
	retrieved, err := store.GetByID(userID, id)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	// LastRunAt should be set
	if retrieved.LastRunAt.IsZero() {
		t.Errorf("expected LastRunAt to be set")
	}

	// NextRunAt should be updated (computed from schedule)
	if retrieved.NextRunAt.IsZero() {
		t.Errorf("expected NextRunAt to be set")
	}

	// For daily schedule, next run should be roughly 24h from now
	expectedNextRun := lastRunTime.Add(24 * time.Hour)
	diff := retrieved.NextRunAt.Sub(expectedNextRun).Abs()
	if diff > 1*time.Second {
		t.Errorf("NextRunAt should be ~24h from lastRunAt, got %v", retrieved.NextRunAt)
	}
}

// TestSQLiteDelete tests deletion via SQLite.
func TestSQLiteDelete(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore failed: %v", err)
	}

	userID := "user1"

	task := ScheduledTask{
		UserID:    userID,
		Name:      "Test task",
		Schedule:  "daily",
		SkillPath: "/path",
		Enabled:   true,
		NextRunAt: time.Now(),
	}

	id, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Delete the task
	err = store.DeleteByID(userID, id)
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// Verify it's deleted
	_, err = store.GetByID(userID, id)
	if err == nil {
		t.Errorf("expected task not found after delete")
	}

	tasks, err := store.List(userID)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

// TestInMemoryTaskStore tests the in-memory store.
func TestInMemoryTaskStore(t *testing.T) {
	store := NewInMemoryTaskStore()
	userID := "user1"

	task := ScheduledTask{
		ID:        "stask-test",
		UserID:    userID,
		Name:      "Test",
		Schedule:  "daily",
		SkillPath: "/path",
		Enabled:   true,
		NextRunAt: time.Now(),
	}

	id, err := store.Add(task)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	retrieved, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != "Test" {
		t.Errorf("expected name Test, got %q", retrieved.Name)
	}

	if retrieved.CreatedAt.IsZero() {
		t.Errorf("expected CreatedAt to be set")
	}
}
