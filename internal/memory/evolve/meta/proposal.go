package meta

import (
	"encoding/json"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

type OrchestratorPatch struct {
	Role    string `json:"role"`    // "planner", "worker", "judge"
	Content string `json:"content"` // markdown body for {role}-evolved.md
}

// NoteProposal describes an atomic note to be written into the NoteStore.
// Each note captures one piece of learned knowledge (Zettelkasten principle).
type NoteProposal struct {
	Content     string   `json:"content"`     // Full note body — what was learned
	Keywords    []string `json:"keywords"`    // Retrieval keywords (exact, lowercase)
	Tags        []string `json:"tags"`        // Topic tags matching the taxonomy
	Description string   `json:"description"` // One-line summary ≤120 chars
}

type EvolutionProposal struct {
	Reasoning           string                `json:"reasoning"`
	ConfigChanges       *evolve.PipelineConfig `json:"config_changes"`
	SkillChanges        []SkillProposal        `json:"skill_changes"`
	AgentsMDPatch       string                 `json:"agents_md_patch"`
	NoteProposals       []NoteProposal         `json:"note_proposals"`
	Summary             string                 `json:"summary"`
	OrchestratorPatches []OrchestratorPatch    `json:"orchestrator_patches"`
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
