package skills

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/pirun"
)

// SkillAgent is a short-lived pi subprocess that selects the best skill
// from a catalog for a given query.
type SkillAgent struct {
	piCfg  config.PiConfig
	secCfg config.SecurityConfig
}

// NewSkillAgent creates a new SkillAgent.
func NewSkillAgent(piCfg config.PiConfig, secCfg config.SecurityConfig) *SkillAgent {
	return &SkillAgent{piCfg: piCfg, secCfg: secCfg}
}

// Find selects the best skill for the given query from the catalog.
// Returns the matching skill name, or "" if no skill is relevant.
func (a *SkillAgent) Find(ctx context.Context, query string, catalog []SkillCatalogEntry) (string, error) {
	if len(catalog) == 0 {
		return "", nil
	}
	prompt := buildSkillSelectionPrompt(query, catalog)
	response, err := pirun.RunTextSession(ctx, a.piCfg, a.secCfg, os.TempDir(), "skill-find", prompt)
	if err != nil {
		return "", fmt.Errorf("skill agent: %w", err)
	}
	return parseSkillResponse(response, catalog), nil
}

func buildSkillSelectionPrompt(query string, catalog []SkillCatalogEntry) string {
	var sb strings.Builder
	sb.WriteString("You are a skill selector. Given a task description and a catalog of available skills,\n")
	sb.WriteString("return the name of the single most relevant skill, or \"none\" if no skill applies.\n\n")
	sb.WriteString("Respond with ONLY the skill name or \"none\". No explanation.\n\n")
	sb.WriteString("Task: ")
	sb.WriteString(query)
	sb.WriteString("\n\nAvailable skills:\n")
	for _, entry := range catalog {
		sb.WriteString(fmt.Sprintf("- name: %s\n  description: %s\n  tags: %v\n\n", entry.Name, entry.Description, entry.Tags))
	}
	return sb.String()
}

// parseSkillResponse extracts the first valid skill name from the pi response.
// Returns "" if the response is "none" or no valid name is found.
func parseSkillResponse(response string, catalog []SkillCatalogEntry) string {
	response = strings.TrimSpace(response)
	if strings.EqualFold(response, "none") {
		return ""
	}
	// Build a set of valid names for quick lookup
	valid := make(map[string]string, len(catalog)) // lowercase name -> original name
	for _, entry := range catalog {
		valid[strings.ToLower(entry.Name)] = entry.Name
	}
	// Check if the full trimmed response is a valid name
	if name, ok := valid[strings.ToLower(response)]; ok {
		return name
	}
	// Scan line by line for a valid name (agent may have added extra whitespace)
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if name, ok := valid[strings.ToLower(line)]; ok {
			return name
		}
	}
	return ""
}
