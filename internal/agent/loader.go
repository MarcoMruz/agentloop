package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentInstruction holds the parsed metadata and content of an agent instruction file.
type AgentInstruction struct {
	Name        string `yaml:"name"`
	Role        string `yaml:"role"`
	Description string `yaml:"description"`
	Content     string `yaml:"-"` // markdown body
	Source      string `yaml:"-"` // "project", "default", "evolved"
}

// AgentLoader discovers and loads role-specific instruction files for Planner,
// Worker, and Judge agents. It checks the project's working directory first,
// then falls back to vault defaults.
type AgentLoader struct {
	vaultDir string
}

// NewAgentLoader creates an AgentLoader that reads vault defaults from vaultDir.
func NewAgentLoader(vaultDir string) *AgentLoader {
	return &AgentLoader{vaultDir: vaultDir}
}

// Load returns all AgentInstructions for the given role and workDir.
//
// Resolution order:
//  1. {workDir}/agents/{role}-*.md  → Source: "project"
//  2. {vaultDir}/agents/{role}-default.md → Source: "default"  (only when no project agents found)
//  3. {vaultDir}/agents/{role}-evolved.md → Source: "evolved"  (always included)
func (l *AgentLoader) Load(role string, workDir string) []AgentInstruction {
	vaultAgentsDir := filepath.Join(l.vaultDir, "agents")

	// Always collect evolved defaults.
	evolved := l.scanSingleFile(vaultAgentsDir, role+"-evolved.md", "evolved")

	// Try project-specific agents first.
	projectDir := filepath.Join(workDir, "agents")
	project := l.scanDir(projectDir, role, "project")

	if len(project) > 0 {
		// Project agents found: return project + evolved.
		return append(project, evolved...)
	}

	// No project agents: use vault default + evolved.
	defaults := l.scanSingleFile(vaultAgentsDir, role+"-default.md", "default")
	return append(defaults, evolved...)
}

// scanDir reads all {role}-*.md files from dir, parses them, and returns them
// sorted alphabetically by filename.
func (l *AgentLoader) scanDir(dir string, role string, source string) []AgentInstruction {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("loader: cannot read dir", "dir", dir, "err", err)
		return nil
	}

	prefix := role + "-"
	var results []AgentInstruction
	var names []string

	// Collect matching filenames first so we can sort them.
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".md") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(dir, name)
		inst := l.parseFile(path, source)
		if inst != nil {
			results = append(results, *inst)
		}
	}
	return results
}

// scanSingleFile loads a single named file from dir (if it exists) and returns it as a slice.
func (l *AgentLoader) scanSingleFile(dir, filename, source string) []AgentInstruction {
	path := filepath.Join(dir, filename)
	inst := l.parseFile(path, source)
	if inst == nil {
		return nil
	}
	return []AgentInstruction{*inst}
}

// parseFile reads a markdown file with YAML frontmatter and returns an AgentInstruction.
// Returns nil if the file cannot be read or does not have valid frontmatter.
func (l *AgentLoader) parseFile(path string, source string) *AgentInstruction {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)

	if !strings.HasPrefix(content, "---") {
		return nil
	}
	// Split off leading "---", then split on the closing "---".
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 {
		return nil
	}

	var inst AgentInstruction
	if err := yaml.Unmarshal([]byte(parts[0]), &inst); err != nil {
		slog.Debug("loader: invalid frontmatter", "path", path, "err", err)
		return nil
	}
	if inst.Name == "" {
		base := filepath.Base(path)
		inst.Name = strings.TrimSuffix(base, ".md")
	}
	inst.Content = strings.TrimSpace(parts[1])
	inst.Source = source
	return &inst
}
