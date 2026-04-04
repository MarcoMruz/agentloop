package metrics

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type EvolutionTriggerFunc func(outcome TaskOutcome)

type Collector struct {
	mu                 sync.Mutex
	vaultPath          string
	scoreThreshold     float64
	minCooldownSeconds int
	maxDailyRuns       int
	lastTriggerTime    time.Time
	dailyTriggerCount  int
	dailyTriggerDate   string
	trigger            EvolutionTriggerFunc
}

func NewCollector(vaultPath string, scoreThreshold float64, minCooldownSeconds, maxDailyRuns int) *Collector {
	metricsDir := filepath.Join(vaultPath, "metrics")
	os.MkdirAll(metricsDir, 0755)

	return &Collector{
		vaultPath:          vaultPath,
		scoreThreshold:     scoreThreshold,
		minCooldownSeconds: minCooldownSeconds,
		maxDailyRuns:       maxDailyRuns,
	}
}

func (c *Collector) SetEvolutionTrigger(fn EvolutionTriggerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trigger = fn
}

func (c *Collector) Record(outcome TaskOutcome) {
	c.persist(outcome)

	score := outcome.Score()
	if score >= c.scoreThreshold {
		return
	}

	c.maybeFireTrigger(outcome)
}

// RecordFeedback persists explicit user feedback and always triggers evolution
// (bypassing the score threshold), subject to cooldown and daily cap.
func (c *Collector) RecordFeedback(feedback UserFeedback) {
	if feedback.Timestamp.IsZero() {
		feedback.Timestamp = time.Now()
	}

	c.persistFeedback(feedback)

	// Enrich the matching TaskOutcome with the feedback text if found.
	outcome := c.findAndEnrichOutcome(feedback)

	c.maybeFireTrigger(outcome)
}

// maybeFireTrigger fires the evolution trigger if rate limits allow.
// Must NOT be called with c.mu already held.
func (c *Collector) maybeFireTrigger(outcome TaskOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.trigger == nil {
		return
	}

	now := time.Now()

	if c.minCooldownSeconds > 0 {
		elapsed := now.Sub(c.lastTriggerTime)
		if elapsed < time.Duration(c.minCooldownSeconds)*time.Second {
			slog.Debug("evolution rate-limited (cooldown)", "elapsed", elapsed)
			return
		}
	}

	today := now.Format("2006-01-02")
	if c.dailyTriggerDate != today {
		c.dailyTriggerDate = today
		c.dailyTriggerCount = 0
	}
	if c.dailyTriggerCount >= c.maxDailyRuns {
		slog.Debug("evolution rate-limited (daily cap)", "count", c.dailyTriggerCount)
		return
	}

	c.lastTriggerTime = now
	c.dailyTriggerCount++

	trigger := c.trigger
	go trigger(outcome)
}

// persistFeedback writes a UserFeedback entry to the per-user feedback JSONL file.
func (c *Collector) persistFeedback(feedback UserFeedback) {
	dir := filepath.Join(c.vaultPath, "metrics")
	os.MkdirAll(dir, 0755)

	dateStr := feedback.Timestamp.Format("2006-01-02")
	path := filepath.Join(dir, feedback.UserID+"-feedback-"+dateStr+".jsonl")

	data, err := json.Marshal(feedback)
	if err != nil {
		slog.Error("failed to marshal feedback", "error", err)
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("failed to open feedback file", "error", err)
		return
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
}

// findAndEnrichOutcome searches recent TaskOutcome files for a matching sessionID.
// If found, the outcome's Feedback field is populated and it is re-persisted.
// Returns the enriched outcome (or a minimal synthetic one if not found).
func (c *Collector) findAndEnrichOutcome(feedback UserFeedback) TaskOutcome {
	outcomes, _ := LoadOutcomes(c.vaultPath, feedback.UserID, 7)
	for i := range outcomes {
		if outcomes[i].SessionID == feedback.SessionID {
			outcomes[i].Feedback = feedback.FeedbackText
			c.persist(outcomes[i])
			return outcomes[i]
		}
	}
	// No matching outcome — synthesise one so the trigger still carries context.
	return TaskOutcome{
		SessionID:    feedback.SessionID,
		UserID:       feedback.UserID,
		Timestamp:    feedback.Timestamp,
		Feedback:     feedback.FeedbackText,
		TaskKeywords: feedback.TaskKeywords,
		TaskTopics:   feedback.TaskTopics,
	}
}

// LoadFeedback loads UserFeedback entries for a user from the last maxDays days.
func LoadFeedback(vaultPath, userId string, maxDays int) ([]UserFeedback, error) {
	dir := filepath.Join(vaultPath, "metrics")
	var feedbacks []UserFeedback

	for d := 0; d < maxDays; d++ {
		date := time.Now().AddDate(0, 0, -d).Format("2006-01-02")
		path := filepath.Join(dir, userId+"-feedback-"+date+".jsonl")

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		for _, line := range splitLines(data) {
			if len(line) == 0 {
				continue
			}
			var fb UserFeedback
			if err := json.Unmarshal(line, &fb); err != nil {
				continue
			}
			feedbacks = append(feedbacks, fb)
		}
	}

	return feedbacks, nil
}

func (c *Collector) persist(outcome TaskOutcome) {
	if outcome.Timestamp.IsZero() {
		outcome.Timestamp = time.Now()
	}
	dateStr := outcome.Timestamp.Format("2006-01-02")
	dir := filepath.Join(c.vaultPath, "metrics")
	os.MkdirAll(dir, 0755)

	path := filepath.Join(dir, outcome.UserID+"-"+dateStr+".jsonl")
	data, err := json.Marshal(outcome)
	if err != nil {
		slog.Error("failed to marshal outcome", "error", err)
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("failed to open metrics file", "error", err)
		return
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
}

func LoadOutcomes(vaultPath, userId string, maxDays int) ([]TaskOutcome, error) {
	dir := filepath.Join(vaultPath, "metrics")
	var outcomes []TaskOutcome

	for d := 0; d < maxDays; d++ {
		date := time.Now().AddDate(0, 0, -d).Format("2006-01-02")
		path := filepath.Join(dir, userId+"-"+date+".jsonl")

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		lines := splitLines(data)
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			var o TaskOutcome
			if err := json.Unmarshal(line, &o); err != nil {
				continue
			}
			outcomes = append(outcomes, o)
		}
	}

	return outcomes, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
