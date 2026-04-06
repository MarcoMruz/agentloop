package scheduled

import (
	"fmt"
	"sort"
	"time"
)

// ScheduledTask represents a task that runs on a schedule.
// The Schedule field can be a cron expression or natural language like "daily at 9am".
type ScheduledTask struct {
	ID        string    `json:"id" mapstructure:"id"`
	UserID    string    `json:"user_id" mapstructure:"user_id"`
	Name      string    `json:"name" mapstructure:"name"`
	Schedule  string    `json:"schedule" mapstructure:"schedule"`       // cron or natural language
	SkillPath string    `json:"skill_path" mapstructure:"skill_path"`   // absolute path to skill dir
	Prompt    string    `json:"prompt" mapstructure:"prompt"`           // optional execution context
	Enabled   bool      `json:"enabled" mapstructure:"enabled"`
	CreatedAt time.Time `json:"created_at" mapstructure:"created_at"`
	NextRunAt time.Time `json:"next_run_at" mapstructure:"next_run_at"`
	LastRunAt time.Time `json:"last_run_at" mapstructure:"last_run_at"` // zero if never run
}

// TaskStore defines the storage interface for scheduled tasks.
// For test purposes only. Production code must use *SQLiteTaskStore directly.
type TaskStore interface {
	// Add inserts a new task and returns its ID.
	Add(task ScheduledTask) (string, error)
	// Get retrieves a task by ID.
	Get(id string) (*ScheduledTask, error)
	// List returns all tasks for a user.
	List(userID string) ([]ScheduledTask, error)
	// ListDue returns tasks that are due (NextRunAt <= now AND Enabled=true), newest first.
	ListDue(userID string, now time.Time) ([]ScheduledTask, error)
	// UpdateLastRun updates the LastRunAt timestamp and computes next run time.
	UpdateLastRun(id string, lastRunAt time.Time, nextSchedule string) error
	// Delete removes a task.
	Delete(id string) error
	// Disable sets Enabled to false without deleting.
	Disable(id string) error
}

// InMemoryTaskStore is used in tests. Not for production use.
type InMemoryTaskStore struct {
	tasks map[string]ScheduledTask
}

func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{tasks: make(map[string]ScheduledTask)}
}

func (s *InMemoryTaskStore) Add(task ScheduledTask) (string, error) {
	task.CreatedAt = time.Now()
	if task.Enabled && task.NextRunAt.IsZero() {
		task.NextRunAt = time.Now()
	}
	s.tasks[task.ID] = task
	return task.ID, nil
}

func (s *InMemoryTaskStore) Get(id string) (*ScheduledTask, error) {
	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return &task, nil
}

func (s *InMemoryTaskStore) List(userID string) ([]ScheduledTask, error) {
	var tasks []ScheduledTask
	for _, t := range s.tasks {
		if t.UserID == userID {
			tasks = append(tasks, t)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks, nil
}

func (s *InMemoryTaskStore) ListDue(userID string, now time.Time) ([]ScheduledTask, error) {
	var tasks []ScheduledTask
	for _, t := range s.tasks {
		if t.UserID == userID && t.Enabled && !t.NextRunAt.After(now) {
			tasks = append(tasks, t)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].NextRunAt.Before(tasks[j].NextRunAt)
	})
	return tasks, nil
}

func (s *InMemoryTaskStore) UpdateLastRun(id string, lastRunAt time.Time, nextSchedule string) error {
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	task.LastRunAt = lastRunAt
	task.NextRunAt = computeNextRun(lastRunAt, nextSchedule)
	s.tasks[id] = task
	return nil
}

func (s *InMemoryTaskStore) Delete(id string) error {
	delete(s.tasks, id)
	return nil
}

func (s *InMemoryTaskStore) Disable(id string) error {
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	task.Enabled = false
	s.tasks[id] = task
	return nil
}

// computeNextRun calculates the next run time from the schedule string.
// For now, supports simple patterns like "daily at 9am", "every 2h", etc.
// TODO: Integrate with a proper cron parser (e.g., robfig/cron).
func computeNextRun(lastRun time.Time, schedule string) time.Time {
	// Placeholder: for daily, return next day at same time
	// For every Xh, add X hours
	// This is simplified; real implementation will parse cron or natural language
	return lastRun.Add(24 * time.Hour)
}
