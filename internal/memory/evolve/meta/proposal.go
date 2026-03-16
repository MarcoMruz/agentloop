package meta

import (
	"encoding/json"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

type EvolutionProposal struct {
	Reasoning     string                `json:"reasoning"`
	ConfigChanges *evolve.PipelineConfig `json:"config_changes"`
	SkillChanges  []SkillProposal       `json:"skill_changes"`
	AgentsMDPatch string                `json:"agents_md_patch"`
	Summary       string                `json:"summary"`
}

type SkillProposal struct {
	Action      string   `json:"action"`
	Name        string   `json:"name"`
	Triggers    []string `json:"triggers"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
}

type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
}

func ParseProposal(text string) (*EvolutionProposal, error) {
	start := -1
	end := -1
	depth := 0
	for i, c := range text {
		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}
	if start == -1 || end == -1 {
		return nil, &json.SyntaxError{}
	}

	var proposal EvolutionProposal
	if err := json.Unmarshal([]byte(text[start:end]), &proposal); err != nil {
		return nil, err
	}
	return &proposal, nil
}
