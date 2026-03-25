package agent

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory"
)

// PromptBuilder constructs the full prompt sent to pi.
// It orders sections for maximum prompt cache efficiency:
//  1. User profile (rarely changes → cached)
//  2. Compacted conversation history (changes slowly)
//  3. Current task (always new)
type PromptBuilder struct {
	mem *memory.Engine
}

func NewPromptBuilder(mem *memory.Engine) *PromptBuilder {
	return &PromptBuilder{mem: mem}
}

// Build constructs the full prompt for a user task.
func (pb *PromptBuilder) Build(userId string, task string, conversationContextID string) (string, error) {
	var sections []string

	// Section 1: Memory context. For Slack threads, scope to the thread context.
	// Falls back to task-aware retrieval for CLI sessions.
	var memCtx string
	var err error
	if conversationContextID != "" {
		memCtx, err = pb.mem.GetContextForUserAndConversationContext(userId, conversationContextID)
	} else {
		memCtx, err = pb.mem.GetContextForUserWithTask(userId, task)
	}
	if err != nil {
		slog.Warn("prompt_builder: failed to get memory context", "userId", userId, "err", err)
	} else if memCtx != "" {
		slog.Debug("prompt_builder: memory context loaded", "userId", userId, "memCtxLen", len(memCtx))
		sections = append(sections, fmt.Sprintf("<memory>\n%s\n</memory>", memCtx))
	} else {
		slog.Debug("prompt_builder: memory context empty, skipping", "userId", userId)
	}

	// Section 2: Task
	sections = append(sections, task)
	prompt := strings.Join(sections, "\n\n")

	return prompt, nil
}
