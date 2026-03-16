package baseline

import (
	"context"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineManager wraps the existing Compactor into the evolve.Manager interface.
type BaselineManager struct {
	compactor *memory.Compactor
}

// NewBaselineManager creates a manager backed by the legacy Compactor.
func NewBaselineManager(compactor *memory.Compactor) *BaselineManager {
	return &BaselineManager{compactor: compactor}
}

// Compact delegates to the underlying Compactor, translating between the
// evolve and memory CompactionResult types.
func (m *BaselineManager) Compact(ctx context.Context, input evolve.CompactionInput) (*evolve.CompactionResult, error) {
	result := m.compactor.Compact(input.Text, input.MaxTokens)
	return &evolve.CompactionResult{
		Text:           result.Text,
		TokenEstimate:  result.TokenEstimate,
		EntriesRemoved: result.EntriesRemoved,
	}, nil
}
