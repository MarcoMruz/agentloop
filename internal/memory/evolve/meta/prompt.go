package meta

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
)

type EvolutionPrompt struct {
	SystemContext string
	Outcomes      []metrics.TaskOutcome
	CurrentConfig *evolve.PipelineConfig
	CurrentSkills []SkillSummary
	AgentsMD      string
	Constraints   []string
}

func BuildEvolutionPrompt(p EvolutionPrompt) string {
	var sb strings.Builder

	sb.WriteString(p.SystemContext)
	sb.WriteString("\n\n")

	sb.WriteString("## Recent Poor Task Outcomes\n\n")
	for i, o := range p.Outcomes {
		sb.WriteString(fmt.Sprintf("### Outcome %d (score: %.2f)\n", i+1, o.Score()))
		sb.WriteString(fmt.Sprintf("- Session: %s\n", o.SessionID))
		sb.WriteString(fmt.Sprintf("- Status: %s\n", o.FinalStatus))
		sb.WriteString(fmt.Sprintf("- HITL Denials: %d\n", o.HITLDenials))
		sb.WriteString(fmt.Sprintf("- Steers: %d\n", o.SteerCount))
		sb.WriteString(fmt.Sprintf("- Tokens: %d\n", o.TokensUsed))
		sb.WriteString(fmt.Sprintf("- Tool Calls: %d\n", o.ToolCalls))
		if len(o.TaskTopics) > 0 {
			sb.WriteString(fmt.Sprintf("- Topics: %s\n", strings.Join(o.TaskTopics, ", ")))
		}
		if len(o.TaskKeywords) > 0 {
			sb.WriteString(fmt.Sprintf("- Keywords: %s\n", strings.Join(o.TaskKeywords, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Current Pipeline Configuration\n\n```json\n")
	cfgJSON, _ := json.MarshalIndent(p.CurrentConfig, "", "  ")
	sb.Write(cfgJSON)
	sb.WriteString("\n```\n\n")

	if len(p.CurrentSkills) > 0 {
		sb.WriteString("## Active Skills\n\n")
		for _, s := range p.CurrentSkills {
			sb.WriteString(fmt.Sprintf("- **%s**: %s (triggers: %s)\n", s.Name, s.Description, strings.Join(s.Triggers, ", ")))
		}
		sb.WriteString("\n")
	}

	if p.AgentsMD != "" {
		sb.WriteString("## Current AGENTS.md Evolved Section\n\n")
		sb.WriteString(p.AgentsMD)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Constraints\n\n")
	for _, c := range p.Constraints {
		sb.WriteString(fmt.Sprintf("- %s\n", c))
	}
	sb.WriteString("\n")

	sb.WriteString("## Required Response Format\n\n")
	sb.WriteString("Respond with a single JSON object:\n\n```json\n")
	sb.WriteString(`{
  "reasoning": "Why these changes will help",
  "config_changes": { ... pipeline config fields to change ... },
  "skill_changes": [
    {"action": "create|update|delete", "name": "evolved-NAME", "triggers": [...], "description": "...", "content": "..."}
  ],
  "agents_md_patch": "New content for the EVOLVED section (or empty)",
  "summary": "One-line summary for git commit"
}`)
	sb.WriteString("\n```\n")

	return sb.String()
}

func DefaultSystemContext() string {
	return `You are MemEvolve, a meta-evolution agent for the AgentLoop system.

Your job: analyze poor task outcomes and propose improvements to make future tasks succeed.

You can propose three types of changes:
1. **Pipeline config changes** — adjust retrieval parameters, compaction strategy, keyword limits, topic extensions
2. **Skill changes** — create or update skills (behavioral patterns) that will be loaded into future agent prompts
3. **AGENTS.md changes** — add learned patterns to the agent's core instructions

Guidelines:
- Focus on the specific topic cluster of failures. Don't make broad changes for narrow problems.
- Skills should have precise triggers so they only activate for relevant tasks.
- AGENTS.md changes should be concise bullet points, not lengthy instructions.
- Config changes should be conservative — small parameter tweaks, not radical rewrites.
- All skill names MUST be prefixed with "evolved-".`
}
