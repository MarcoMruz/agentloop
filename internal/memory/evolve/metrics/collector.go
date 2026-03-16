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
