package baseline

import (
	"context"
	"time"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineEncoder wraps existing heuristic-based profile updates and keyword/topic
// extraction into the evolve.Encoder interface.
type BaselineEncoder struct {
	profiles *memory.ProfileStore
}

// NewBaselineEncoder creates an encoder that delegates to ProfileStore and
// the existing ExtractKeywords / ExtractTopics helpers.
func NewBaselineEncoder(profiles *memory.ProfileStore) *BaselineEncoder {
	return &BaselineEncoder{profiles: profiles}
}

// Encode processes a raw interaction into MemoryUnits. It updates the user
// profile as a side-effect (same as the legacy path) and produces one unit
// per non-empty message (user and/or assistant).
func (e *BaselineEncoder) Encode(ctx context.Context, input evolve.EncoderInput) (*evolve.EncoderOutput, error) {
	e.profiles.UpdateFromInteraction(input.UserID, input.UserMessage, input.ToolsUsed)

	now := time.Now()
	units := make([]evolve.MemoryUnit, 0, 2)

	if input.UserMessage != "" {
		units = append(units, evolve.MemoryUnit{
			ID:        input.UserID + "-" + now.Format("150405") + "-user",
			Timestamp: now,
			Role:      "user",
			Content:   input.UserMessage,
			Keywords:  memory.ExtractKeywords(input.UserMessage),
			Topics:    memory.ExtractTopics(input.UserMessage),
			Metadata: map[string]string{
				"type":      "conversation",
				"contextID": input.ConversationContextID,
				"userId":    input.UserID,
			},
		})
	}

	if input.AgentReply != "" {
		units = append(units, evolve.MemoryUnit{
			ID:        input.UserID + "-" + now.Format("150405") + "-assistant",
			Timestamp: now,
			Role:      "assistant",
			Content:   input.AgentReply,
			Keywords:  memory.ExtractKeywords(input.AgentReply),
			Topics:    memory.ExtractTopics(input.AgentReply),
			Metadata: map[string]string{
				"type":      "conversation",
				"contextID": input.ConversationContextID,
				"userId":    input.UserID,
			},
		})
	}

	return &evolve.EncoderOutput{Units: units}, nil
}
