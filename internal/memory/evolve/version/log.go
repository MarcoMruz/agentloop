package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type LogEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	SessionID     string    `json:"session_id"`
	Score         float64   `json:"score"`
	Summary       string    `json:"summary"`
	ConfigVersion int       `json:"config_version"`
}

type EvolutionLog struct {
	path string
}

func NewEvolutionLog(evolvedDir string) *EvolutionLog {
	return &EvolutionLog{path: filepath.Join(evolvedDir, "evolution-log.jsonl")}
}

func (l *EvolutionLog) Append(entry LogEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
	return nil
}
