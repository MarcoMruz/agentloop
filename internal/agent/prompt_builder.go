package agent

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/skills"
)

// PromptBuilder constructs the full prompt sent to pi.
// It orders sections for maximum prompt cache efficiency:
//  1. User profile (rarely changes → cached)
//  2. Active skills (changes per-task, but often repeated)
//  3. Compacted conversation history (changes slowly)
//  4. Current task (always new)
type PromptBuilder struct {
	mem    *memory.Engine
	skills *skills.Registry
}

func NewPromptBuilder(mem *memory.Engine, sk *skills.Registry) *PromptBuilder {
	return &PromptBuilder{mem: mem, skills: sk}
}

// Build constructs the full prompt for a user task.
func (pb *PromptBuilder) Build(userId string, task string, skillNames []string) (string, error) {
	var sections []string

	// Section 1: Memory context — task-aware relevance filtering keeps token cost low.
	// Falls back to full context when no index exists yet.
	memCtx, err := pb.mem.GetContextForUserWithTask(userId, task)
	if err != nil {
		slog.Warn("prompt_builder: failed to get memory context", "userId", userId, "err", err)
	} else if memCtx != "" {
		slog.Debug("prompt_builder: memory context loaded", "userId", userId, "memCtxLen", len(memCtx))
		sections = append(sections, fmt.Sprintf("<memory>\n%s\n</memory>", memCtx))
	} else {
		slog.Debug("prompt_builder: memory context empty, skipping", "userId", userId)
	}

	// Section 2: Skills (loaded on demand)
	for _, name := range skillNames {
		skill, err := pb.skills.Get(name)
		if err != nil {
			slog.Warn("prompt_builder: skill not found", "skill", name, "err", err)
			continue
		}
		sections = append(sections, fmt.Sprintf("<skill name=%q>\n%s\n</skill>", name, skill.Instructions))
	}

	// Section 3: Task
	sections = append(sections, task)
	prompt := strings.Join(sections, "\n\n")

	return prompt, nil
}

// DetectSkills analyzes the task text and returns relevant skill names.
// Simple keyword matching — no LLM call needed.
func (pb *PromptBuilder) DetectSkills(task string) []string {
	lower := strings.ToLower(task)
	var matched []string

	allSkills := pb.skills.List()
	slog.Debug("prompt_builder: DetectSkills scanning", "availableSkills", len(allSkills))

	for _, sk := range allSkills {
		for _, trigger := range sk.Triggers {
			if strings.Contains(lower, strings.ToLower(trigger)) {
				slog.Debug("prompt_builder: skill trigger matched", "skill", sk.Name, "trigger", trigger)
				matched = append(matched, sk.Name)
				break
			}
		}
	}

	slog.Debug("prompt_builder: DetectSkills done", "matched", matched)
	return matched
}
