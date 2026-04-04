package meta

import (
	"encoding/json"
	"fmt"

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
	// Find the JSON object boundaries
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
		return nil, fmt.Errorf("no complete JSON object found in response (start: %d, end: %d)", start, end)
	}

	jsonText := text[start:end]
	
	// Try to parse the JSON
	var proposal EvolutionProposal
	if err := json.Unmarshal([]byte(jsonText), &proposal); err != nil {
		// Provide more helpful error context
		return nil, fmt.Errorf("JSON parse error: %w\nJSON content: %s", err, truncateJSON(jsonText))
	}
	
	// Validate required fields and provide defaults
	if proposal.Summary == "" {
		proposal.Summary = "MemEvolve update"
	}
	if proposal.Reasoning == "" {
		proposal.Reasoning = "Analysis of task outcomes and proposed improvements"
	}
	
	// Ensure slices are not nil
	if proposal.SkillChanges == nil {
		proposal.SkillChanges = []SkillProposal{}
	}
	if proposal.NoteProposals == nil {
		proposal.NoteProposals = []NoteProposal{}
	}
	if proposal.OrchestratorPatches == nil {
		proposal.OrchestratorPatches = []OrchestratorPatch{}
	}
	
	return &proposal, nil
}

// truncateJSON truncates JSON for error messages while preserving structure info
func truncateJSON(jsonText string) string {
	if len(jsonText) <= 200 {
		return jsonText
	}
	return jsonText[:200] + "..."
}
