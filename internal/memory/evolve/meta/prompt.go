package meta

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
)

type OrchestratorEvolutionSignals struct {
	Iterations          int
	FinalPass           bool
	PlanStepCount       int
	ActualStepsNeeded   int
	MaxSummaryTokens    int
	JudgeGapSpecificity int
	EvolutionCount      int
	WorkerSummaries     []string
	JudgeGaps           []string
}

type EvolutionPrompt struct {
	SystemContext       string
	Outcomes            []metrics.TaskOutcome
	CurrentConfig       *evolve.PipelineConfig
	CurrentSkills       []SkillSummary
	AgentsMD            string
	Constraints         []string
	OrchestratorSignals *OrchestratorEvolutionSignals
	UserFeedback        string
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

	if p.OrchestratorSignals != nil {
		s := p.OrchestratorSignals
		sb.WriteString("## Orchestrator Signals\n\n")
		sb.WriteString(fmt.Sprintf("- Iterations: %d\n", s.Iterations))
		sb.WriteString(fmt.Sprintf("- Final Pass: %v\n", s.FinalPass))
		sb.WriteString(fmt.Sprintf("- Plan Steps: %d (actual needed: %d)\n", s.PlanStepCount, s.ActualStepsNeeded))
		sb.WriteString(fmt.Sprintf("- Max Worker Summary Tokens: %d\n", s.MaxSummaryTokens))
		sb.WriteString(fmt.Sprintf("- Judge Gap Specificity: %d/5\n", s.JudgeGapSpecificity))
		sb.WriteString(fmt.Sprintf("- Evolutions This Run: %d\n", s.EvolutionCount))
		if len(s.JudgeGaps) > 0 {
			sb.WriteString("\n### Judge Gaps (failed iterations)\n\n")
			for _, g := range s.JudgeGaps {
				sb.WriteString(fmt.Sprintf("- %s\n", g))
			}
		}
		sb.WriteString("\n")
	}

	if p.UserFeedback != "" {
		sb.WriteString("## User Feedback\n\n")
		sb.WriteString(p.UserFeedback)
		sb.WriteString("\n\n")
		sb.WriteString("The user reported incorrect/unexpected results. Analyze what went wrong and propose changes that prevent this class of mistake in future tasks. Focus on:\n")
		sb.WriteString("1) What knowledge was missing\n")
		sb.WriteString("2) What rule/skill would prevent recurrence\n")
		sb.WriteString("3) What notes to create for future context\n\n")
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
	sb.WriteString("Respond with a single JSON object. All fields are required, use null or empty arrays/strings if no changes are needed.\n\n```json\n")
	sb.WriteString(`{
  "reasoning": "Explain why these changes will help improve task outcomes",
  "config_changes": {
    "version": 2,
    "retriever": {
      "keyword_weight": 0.4,
      "topic_weight": 0.6,
      "max_results": 10,
      "position": "edges"
    }
  },
  "skill_changes": [
    {
      "action": "create",
      "name": "evolved-example-skill",
      "triggers": ["example", "demo"],
      "description": "Example skill description",
      "content": "# Example Skill\n\nSkill instructions here."
    }
  ],
  "agents_md_patch": "## New Pattern\n\n- Bullet point with learned behavior",
  "note_proposals": [
    {
      "content": "Complete note body stating what was learned and when to apply it. One atomic idea only.",
      "keywords": ["exact", "lowercase", "retrieval", "terms"],
      "tags": ["relevant-topic-from-taxonomy"],
      "description": "One-line summary of the knowledge (≤120 chars)"
    }
  ],
  "summary": "Brief git commit message describing the evolution",
  "orchestrator_patches": [
    {
      "role": "worker",
      "content": "Updated instructions for worker agents to improve execution patterns"
    }
  ]
}`)
	sb.WriteString("\n```\n\n")
	sb.WriteString("**IMPORTANT JSON Requirements:**\n")
	sb.WriteString("- Provide exactly one valid JSON object\n")
	sb.WriteString("- All fields are required (use null, empty string \"\", or empty array [] if no changes)\n")
	sb.WriteString("- No trailing commas\n")
	sb.WriteString("- Use proper JSON syntax - all strings must be quoted\n")
	sb.WriteString("- Set config_changes to null if no pipeline changes are needed\n")

	return sb.String()
}

func DefaultSystemContext() string {
	return `You are MemEvolve, a meta-evolution agent for the AgentLoop system.

Your job: analyze poor task outcomes and propose improvements to make future tasks succeed.

You can propose five types of changes:
1. **Pipeline config changes** — adjust retrieval parameters, compaction strategy, keyword limits, topic extensions
2. **Skill changes** — create or update skills (behavioral patterns) that will be loaded into future agent prompts
3. **AGENTS.md changes** — add learned patterns to the agent's core instructions
4. **Orchestrator agent patches** — update Planner, Worker, or Judge instructions to address systemic issues
   - Planner patches: improve task decomposition, specificity of steps, success criteria
   - Worker patches: improve execution patterns, avoid repeated HITL denials
   - Judge patches: improve gap specificity, evidence requirements
5. **Atomic notes** — write structured knowledge units into the memory store for future retrieval
   - Each note captures exactly ONE learned fact, pattern, or rule (Zettelkasten principle)
   - Notes are retrieved when future tasks share keywords or topics — make them specific and precise
   - A good note answers: "What should the agent know when it encounters this topic again?"
   - Content should be self-contained — no references like "as seen above" or "in this case"
   - Keywords must be exact lowercase terms the agent will use in future task descriptions
   - Tags must come from the topic taxonomy: auth, database, deploy, frontend, backend, test,
     config, docker, migration, cache, security, build, kubernetes, monitor, logging, debug,
     refactor, pipeline, email, calendar, meeting, schedule, report, reminder, invoice, budget,
     travel, contact, document, slack, ticket, review, interview, presentation, deadline,
     shopping, finance, payment
   - Description is the retrieval preview (≤120 chars) — make it specific enough to distinguish
     this note from others on the same topic

Guidelines:
- Focus on the specific topic cluster of failures. Don't make broad changes for narrow problems.
- Skills should have precise triggers so they only activate for relevant tasks.
- AGENTS.md changes should be concise bullet points, not lengthy instructions.
- Config changes should be conservative — small parameter tweaks, not radical rewrites.
- All skill names MUST be prefixed with "evolved-".
- Prefer notes over AGENTS.md for domain-specific knowledge — notes are retrieved selectively,
  AGENTS.md is injected into every prompt regardless of relevance.
- Write 1–5 notes per evolution. More is not better; precision beats coverage.`
}
