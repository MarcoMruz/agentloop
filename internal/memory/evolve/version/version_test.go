package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvolutionLogAppend(t *testing.T) {
	dir := t.TempDir()
	log := NewEvolutionLog(dir)

	log.Append(LogEntry{Summary: "first change", ConfigVersion: 1})
	log.Append(LogEntry{Summary: "second change", ConfigVersion: 2})

	data, err := os.ReadFile(filepath.Join(dir, "evolution-log.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "first change") {
		t.Fatal("first entry missing")
	}
	if !strings.Contains(lines[1], "second change") {
		t.Fatal("second entry missing")
	}
}

func TestSnapshotContainsAllFiles(t *testing.T) {
	dir := t.TempDir()
	evolvedDir := filepath.Join(dir, "memory", "evolved")
	os.MkdirAll(evolvedDir, 0755)
	os.WriteFile(filepath.Join(evolvedDir, "pipeline-config.yaml"), []byte("version: 1"), 0644)

	agentsMD := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(agentsMD, []byte("<!-- EVOLVED:START -->\ntest\n<!-- EVOLVED:END -->"), 0644)

	snap := NewSnapshotter(dir, agentsMD)
	ts, err := snap.Take()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	snapshotDir := filepath.Join(evolvedDir, "snapshots", ts)

	if _, err := os.Stat(filepath.Join(snapshotDir, "pipeline-config.yaml")); err != nil {
		t.Fatal("pipeline-config.yaml not in snapshot")
	}

	if _, err := os.Stat(filepath.Join(snapshotDir, "agents-md-section.md")); err != nil {
		t.Fatal("agents-md-section.md not in snapshot")
	}
}
