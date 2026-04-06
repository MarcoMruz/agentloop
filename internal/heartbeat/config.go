package heartbeat

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// HeartbeatConfig represents the heartbeat subsystem configuration.
type HeartbeatConfig struct {
	// Enabled turns on the heartbeat subsystem.
	Enabled bool `yaml:"enabled"`
	// Interval is how often to emit heartbeat events.
	Interval time.Duration `yaml:"interval"`
	// ConsolidationEnabled turns on heartbeat consolidation.
	ConsolidationEnabled bool `yaml:"consolidation_enabled"`
	// ConsolidationInterval is how often consolidation is triggered.
	ConsolidationInterval time.Duration `yaml:"consolidation_interval"`
	// ConsolidationIdleThreshold is the idle time before consolidation can occur.
	ConsolidationIdleThreshold time.Duration `yaml:"consolidation_idle_threshold"`
	// MaxConsolidationDuration limits how long consolidation can run.
	MaxConsolidationDuration time.Duration `yaml:"max_consolidation_duration"`
	// MemoryTools is a list of memory tool names to use in consolidation.
	MemoryTools []string `yaml:"memory_tools"`
	// Path is the location of the heartbeat markdown file (injected at runtime, not from YAML).
	Path string `yaml:"-"`
}

// ParseHeartbeatConfig parses a heartbeat markdown file with YAML frontmatter.
// Returns the parsed HeartbeatConfig and any error encountered.
func ParseHeartbeatConfig(content string) (*HeartbeatConfig, error) {
	if content == "" {
		return nil, fmt.Errorf("empty heartbeat config content")
	}

	yamlBlock, _ := parseFrontmatter(content)
	if yamlBlock == "" {
		return nil, fmt.Errorf("no YAML frontmatter found in heartbeat config")
	}

	var cfg HeartbeatConfig
	if err := yaml.Unmarshal([]byte(yamlBlock), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse heartbeat config YAML: %w", err)
	}

	return &cfg, nil
}

// parseFrontmatter extracts YAML frontmatter from markdown content.
// Returns the YAML block and the body separately.
// The YAML block is the content between the first two "---" delimiters.
func parseFrontmatter(content string) (yamlBlock string, body string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	in, closed := false, false
	var fm, bd []string

	for scanner.Scan() {
		line := scanner.Text()
		if !in && !closed && line == "---" {
			in = true
			continue
		}
		if in && !closed {
			if line == "---" {
				closed = true
				continue
			}
			fm = append(fm, line)
			continue
		}
		bd = append(bd, line)
	}

	return strings.Join(fm, "\n"), strings.Join(bd, "\n")
}
