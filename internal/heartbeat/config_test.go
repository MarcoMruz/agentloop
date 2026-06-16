package heartbeat

import (
	"strings"
	"testing"
	"time"
)

// TestParseHeartbeatConfig tests parsing a valid heartbeat config with all fields.
func TestParseHeartbeatConfig(t *testing.T) {
	content := `---
enabled: true
interval: 30s
consolidation_enabled: true
consolidation_interval: 5m
consolidation_idle_threshold: 1m
max_consolidation_duration: 10m
memory_tools:
  - Add_memory
  - Update_memory
  - Retrieve_memory
---

# Heartbeat Configuration

This is the body of the heartbeat config document.
`

	cfg, err := ParseHeartbeatConfig(content)
	if err != nil {
		t.Fatalf("ParseHeartbeatConfig failed: %v", err)
	}

	if !cfg.Enabled {
		t.Errorf("expected Enabled=true, got %v", cfg.Enabled)
	}

	if cfg.Interval != 30*time.Second {
		t.Errorf("expected Interval=30s, got %v", cfg.Interval)
	}

	if !cfg.ConsolidationEnabled {
		t.Errorf("expected ConsolidationEnabled=true, got %v", cfg.ConsolidationEnabled)
	}

	if cfg.ConsolidationInterval != 5*time.Minute {
		t.Errorf("expected ConsolidationInterval=5m, got %v", cfg.ConsolidationInterval)
	}

	if cfg.ConsolidationIdleThreshold != 1*time.Minute {
		t.Errorf("expected ConsolidationIdleThreshold=1m, got %v", cfg.ConsolidationIdleThreshold)
	}

	if cfg.MaxConsolidationDuration != 10*time.Minute {
		t.Errorf("expected MaxConsolidationDuration=10m, got %v", cfg.MaxConsolidationDuration)
	}

	expectedTools := []string{"Add_memory", "Update_memory", "Retrieve_memory"}
	if len(cfg.MemoryTools) != len(expectedTools) {
		t.Errorf("expected %d memory tools, got %d", len(expectedTools), len(cfg.MemoryTools))
	}
	for i, tool := range expectedTools {
		if cfg.MemoryTools[i] != tool {
			t.Errorf("memory tool %d: expected %q, got %q", i, tool, cfg.MemoryTools[i])
		}
	}
}

// TestParseHeartbeatConfigDefaults tests parsing a minimal config with missing fields.
func TestParseHeartbeatConfigDefaults(t *testing.T) {
	content := `---
enabled: false
interval: 1m
---

# Heartbeat Configuration

Minimal config.
`

	cfg, err := ParseHeartbeatConfig(content)
	if err != nil {
		t.Fatalf("ParseHeartbeatConfig failed: %v", err)
	}

	if cfg.Enabled {
		t.Errorf("expected Enabled=false, got %v", cfg.Enabled)
	}

	if cfg.Interval != 1*time.Minute {
		t.Errorf("expected Interval=1m, got %v", cfg.Interval)
	}

	// Unset fields should be zero-values
	if cfg.ConsolidationEnabled {
		t.Errorf("expected ConsolidationEnabled=false (zero-value), got %v", cfg.ConsolidationEnabled)
	}

	if cfg.ConsolidationInterval != 0 {
		t.Errorf("expected ConsolidationInterval=0 (zero-value), got %v", cfg.ConsolidationInterval)
	}

	if cfg.ConsolidationIdleThreshold != 0 {
		t.Errorf("expected ConsolidationIdleThreshold=0 (zero-value), got %v", cfg.ConsolidationIdleThreshold)
	}

	if cfg.MaxConsolidationDuration != 0 {
		t.Errorf("expected MaxConsolidationDuration=0 (zero-value), got %v", cfg.MaxConsolidationDuration)
	}

	if len(cfg.MemoryTools) != 0 {
		t.Errorf("expected empty MemoryTools, got %v", cfg.MemoryTools)
	}
}

// TestParseHeartbeatConfigInvalid tests parsing invalid YAML.
func TestParseHeartbeatConfigInvalid(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectedErr string
	}{
		{
			name:        "empty content",
			content:     "",
			expectedErr: "empty heartbeat config content",
		},
		{
			name:        "no frontmatter",
			content:     "Just some markdown without frontmatter\n",
			expectedErr: "no YAML frontmatter found",
		},
		{
			name: "invalid YAML",
			content: `---
enabled: true
interval: invalid duration
---
body`,
			expectedErr: "failed to parse heartbeat config YAML",
		},
		{
			name: "malformed interval",
			content: `---
enabled: true
interval: not-a-duration
---
body`,
			expectedErr: "failed to parse heartbeat config YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHeartbeatConfig(tt.content)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.expectedErr)
			}
			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("expected error containing %q, got %q", tt.expectedErr, err.Error())
			}
		})
	}
}
