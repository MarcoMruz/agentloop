package agent

import (
	"fmt"
	"strings"

	"github.com/user/agentloop/internal/memory"
	"github.com/user/agentloop/internal/skills"
)

// PromptBuilder constructs the full prompt sent to pi.
// It orders sections for maximum prompt cache efficiency:
//   1. User profile (rarely changes → cached)
//   2. Active skills (changes per-task, but often repeated)
//   3. Compacted conversation history (changes slowly)
//   4. Current task (always new)
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

	// Section 1: Memory context (stable prefix for caching)
	memCtx, err := pb.mem.GetContextForUser(userId)
	if err == nil && memCtx != "" {
		sections = append(sections, fmt.Sprintf("<memory>\n%s\n</memory>", memCtx))
	}

	// Section 2: Skills (loaded on demand)
	for _, name := range skillNames {
		skill, err := pb.skills.Get(name)
		if err != nil { continue }
		sections = append(sections, fmt.Sprintf("<skill name=%q>\n%s\n</skill>", name, skill.Instructions))
	}

	// Section 3: Task
	sections = append(sections, task)

	return strings.Join(sections, "\n\n"), nil
}

// DetectSkills analyzes the task text and returns relevant skill names.
// Simple keyword matching — no LLM call needed.
func (pb *PromptBuilder) DetectSkills(task string) []string {
	lower := strings.ToLower(task)
	var matched []string

	allSkills := pb.skills.List()
	for _, sk := range allSkills {
		for _, trigger := range sk.Triggers {
			if strings.Contains(lower, strings.ToLower(trigger)) {
				matched = append(matched, sk.Name)
				break
			}
		}
	}
	return matched
}
