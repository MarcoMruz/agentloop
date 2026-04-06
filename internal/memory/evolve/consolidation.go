package evolve

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ConsolidationHandler is a callback invoked by the heartbeat system to perform
// consolidation of memory. It receives the current context and a MemoryToolkit
// containing the 4 core memory operations.
type ConsolidationHandler func(ctx context.Context, toolkit MemoryToolkit) error

// MemoryToolkit holds the 4 core memory operation functions used during consolidation.
type MemoryToolkit struct {
	// Retrieve fetches relevant memory given a query.
	Retrieve func(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error)

	// Encode transforms interactions into structured memory units.
	Encode func(ctx context.Context, input EncoderInput) (*EncoderOutput, error)

	// Store persists memory units to the vault.
	Store func(ctx context.Context, units []MemoryUnit) error

	// Compact optimizes memory under a token budget.
	Compact func(ctx context.Context, input CompactionInput) (*CompactionResult, error)
}

// ConsolidationConfig holds configuration for the consolidation subsystem.
type ConsolidationConfig struct {
	// IdleThreshold is the duration of inactivity that triggers consolidation.
	IdleThreshold time.Duration

	// ConsolidationInterval is the maximum time between consolidations.
	ConsolidationInterval time.Duration

	// MaxConsolidationDuration is the timeout for a single consolidation operation.
	MaxConsolidationDuration time.Duration

	// Enabled controls whether consolidation is active.
	Enabled bool
}

// ConsolidationManager orchestrates memory consolidation with idle-vs-forced decision logic.
type ConsolidationManager struct {
	mu                   sync.Mutex
	config               ConsolidationConfig
	lastConsolidation    time.Time // Track last consolidation time
	lastActivity         time.Time // Track activity for idle detection
	consolidationHandler ConsolidationHandler
	toolkit              MemoryToolkit
}

// NewConsolidationManager creates a new consolidation manager with the given config.
func NewConsolidationManager(config ConsolidationConfig, handler ConsolidationHandler, toolkit MemoryToolkit) *ConsolidationManager {
	now := time.Now()
	return &ConsolidationManager{
		config:            config,
		lastConsolidation: now,
		lastActivity:      now,
		consolidationHandler: handler,
		toolkit:           toolkit,
	}
}

// RecordActivity updates the last activity timestamp.
func (cm *ConsolidationManager) RecordActivity() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.lastActivity = time.Now()
}

// ShouldConsolidate determines whether consolidation should trigger based on idle state
// or forced interval. Returns true if consolidation is needed.
func (cm *ConsolidationManager) ShouldConsolidate() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.config.Enabled {
		return false
	}

	now := time.Now()

	// Trigger on idle: no activity for more than idleThreshold
	idleDuration := now.Sub(cm.lastActivity)
	if idleDuration > cm.config.IdleThreshold {
		return true
	}

	// Trigger on forced interval: consolidation overdue
	timeSinceConsolidation := now.Sub(cm.lastConsolidation)
	if timeSinceConsolidation > cm.config.ConsolidationInterval {
		return true
	}

	return false
}

// ConsolidationTrigger is the reason consolidation was invoked.
type ConsolidationTrigger int

const (
	TriggerIdle   ConsolidationTrigger = iota // Idle threshold exceeded
	TriggerForced                             // Forced consolidation interval exceeded
)

// DetermineConsolidationTrigger returns the reason consolidation should happen,
// or nil if no consolidation is needed.
func (cm *ConsolidationManager) DetermineConsolidationTrigger() *ConsolidationTrigger {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.config.Enabled {
		return nil
	}

	now := time.Now()

	// Trigger on idle: no activity for more than idleThreshold
	idleDuration := now.Sub(cm.lastActivity)
	if idleDuration > cm.config.IdleThreshold {
		trigger := TriggerIdle
		return &trigger
	}

	// Trigger on forced interval: consolidation overdue
	timeSinceConsolidation := now.Sub(cm.lastConsolidation)
	if timeSinceConsolidation > cm.config.ConsolidationInterval {
		trigger := TriggerForced
		return &trigger
	}

	return nil
}

// Consolidate executes the consolidation operation with timeout and panic recovery.
func (cm *ConsolidationManager) Consolidate(ctx context.Context) error {
	if !cm.config.Enabled {
		return nil
	}

	// Check if consolidation should happen
	if !cm.ShouldConsolidate() {
		return nil
	}

	// Create a timeout context for the consolidation operation
	timeoutCtx, cancel := context.WithTimeout(ctx, cm.config.MaxConsolidationDuration)
	defer cancel()

	// Execute with panic recovery
	defer func() {
		if r := recover(); r != nil {
			slog.Error("consolidation panicked", "panic", r)
		}
	}()

	// Execute the handler
	if err := cm.consolidationHandler(timeoutCtx, cm.toolkit); err != nil {
		slog.Warn("consolidation failed", "error", err)
		return err
	}

	// Update last consolidation time after successful consolidation
	cm.mu.Lock()
	cm.lastConsolidation = time.Now()
	cm.mu.Unlock()

	return nil
}

// ConsolidationSystemPrompt is the system prompt template for consolidation operations.
const ConsolidationSystemPrompt = `You are a memory consolidation assistant for AgentLoop. Your role is to analyze recent memory interactions and suggest improvements to the memory structure.

Given the current memory state, you will:

1. Identify patterns and themes across recent interactions
2. Detect redundancies or overlapping information
3. Suggest improvements to memory organization
4. Recommend compression strategies for verbose content
5. Flag potentially stale or low-value memories for pruning

Your consolidation analysis should focus on:
- Extracting key insights from repetitive interactions
- Identifying domain-specific vocabulary or patterns
- Detecting knowledge gaps that should be filled
- Suggesting optimal memory compression strategies
- Recommending priority levels for memory preservation

Provide your analysis in JSON format with the following structure:
{
  "identified_patterns": ["pattern 1", "pattern 2", ...],
  "redundancies": [
    {"description": "...", "affected_ids": ["id1", "id2"]},
    ...
  ],
  "compression_opportunities": [
    {"id": "...", "summary": "...", "compression_ratio": 0.5},
    ...
  ],
  "stale_memories": [
    {"id": "...", "reason": "...", "confidence": 0.8},
    ...
  ],
  "organization_improvements": ["improvement 1", "improvement 2", ...],
  "knowledge_gaps": ["gap 1", "gap 2", ...],
  "summary": "Overall memory consolidation assessment"
}
`

// ConsolidationMetrics tracks statistics about consolidation operations.
type ConsolidationMetrics struct {
	mu                      sync.RWMutex
	totalConsolidations     atomic.Uint32
	successfulConsolidations atomic.Uint32
	failedConsolidations    atomic.Uint32
	totalTimeSpent          atomic.Int64 // nanoseconds
	lastConsolidationTime   time.Time
	lastConsolidationError  error
}

// NewConsolidationMetrics creates a new metrics tracker.
func NewConsolidationMetrics() *ConsolidationMetrics {
	return &ConsolidationMetrics{}
}

// RecordConsolidation records the outcome of a consolidation operation.
func (cm *ConsolidationMetrics) RecordConsolidation(success bool, duration time.Duration) {
	cm.totalConsolidations.Add(1)
	if success {
		cm.successfulConsolidations.Add(1)
	} else {
		cm.failedConsolidations.Add(1)
	}
	cm.totalTimeSpent.Add(duration.Nanoseconds())

	cm.mu.Lock()
	cm.lastConsolidationTime = time.Now()
	cm.mu.Unlock()
}

// RecordConsolidationError records an error from a consolidation operation.
func (cm *ConsolidationMetrics) RecordConsolidationError(err error) {
	cm.failedConsolidations.Add(1)

	cm.mu.Lock()
	cm.lastConsolidationError = err
	cm.lastConsolidationTime = time.Now()
	cm.mu.Unlock()
}

// Stats returns the current consolidation statistics.
func (cm *ConsolidationMetrics) Stats() map[string]interface{} {
	total := cm.totalConsolidations.Load()
	successful := cm.successfulConsolidations.Load()
	failed := cm.failedConsolidations.Load()
	totalTime := cm.totalTimeSpent.Load()

	cm.mu.RLock()
	lastTime := cm.lastConsolidationTime
	lastErr := cm.lastConsolidationError
	cm.mu.RUnlock()

	var avgDuration float64
	if total > 0 {
		avgDuration = float64(totalTime) / float64(total) / 1e6 // Convert to milliseconds
	}

	return map[string]interface{}{
		"total_consolidations":      total,
		"successful_consolidations": successful,
		"failed_consolidations":     failed,
		"average_duration_ms":       avgDuration,
		"last_consolidation_time":   lastTime,
		"last_consolidation_error":  lastErr,
	}
}
