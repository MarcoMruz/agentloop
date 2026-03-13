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
// For skills with explicit triggers, matches by keyword.
// For skills with only a description (e.g. superpowers skills), falls back to
// extracting meaningful words from the description and matching those.
// No LLM call needed.
func (pb *PromptBuilder) DetectSkills(task string) []string {
	lower := strings.ToLower(task)
	var matched []string

	allSkills := pb.skills.List()
	slog.Debug("prompt_builder: DetectSkills scanning", "availableSkills", len(allSkills))

	for _, sk := range allSkills {
		if len(sk.Triggers) > 0 {
			// Explicit trigger keywords — original behaviour.
			for _, trigger := range sk.Triggers {
				if strings.Contains(lower, strings.ToLower(trigger)) {
					slog.Debug("prompt_builder: skill trigger matched", "skill", sk.Name, "trigger", trigger)
					matched = append(matched, sk.Name)
					break
				}
			}
		} else if sk.Description != "" {
			// Fallback: extract content words from description and match against task.
			// Strip common filler phrases like "Use when", "before any", etc.
			descLower := strings.ToLower(sk.Description)
			descLower = strings.NewReplacer(
				"use when", "", "use this", "", "you must use this", "",
				"before any", "", "before ", "", "after ", "",
				"requires ", "", "guides ", "", "explores ", "",
			).Replace(descLower)

			words := strings.FieldsFunc(descLower, func(r rune) bool {
				return r == ' ' || r == ',' || r == '.' || r == '-' || r == '(' || r == ')' || r == '\'' || r == '"'
			})

			for _, word := range words {
				// Only match on meaningful words (length > 4, not stop-words).
				if len(word) <= 4 || isStopWord(word) {
					continue
				}
				// Use a 5-char prefix as a simple stem so that e.g.
				// "creating" matches task word "create", "features" matches "feature".
				stem := word
				if len(word) > 5 {
					stem = word[:5]
				}
				if strings.Contains(lower, stem) {
					slog.Debug("prompt_builder: skill description word matched", "skill", sk.Name, "word", word, "stem", stem)
					matched = append(matched, sk.Name)
					break
				}
			}
		}
	}

	slog.Debug("prompt_builder: DetectSkills done", "matched", matched)
	return matched
}

// isStopWord returns true for common English words that are not useful as skill triggers.
func isStopWord(w string) bool {
	switch w {
	case "about", "above", "after", "again", "against", "being", "between",
		"could", "doing", "during", "each", "every", "from", "further",
		"having", "here", "into", "more", "most", "other", "should",
		"some", "such", "than", "that", "their", "them", "then", "there",
		"these", "they", "this", "those", "through", "under", "until",
		"very", "what", "when", "where", "which", "while", "with",
		"would", "your", "also", "both", "just", "like", "make", "need",
		"only", "over", "same", "take", "well", "will", "work",
		"adding", "based", "given", "including":
		return true
	}
	return false
}
