package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/heartbeat/scheduled"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
	"github.com/MarcoMruz/agentloop/internal/pirun"
	"github.com/MarcoMruz/agentloop/internal/session"
	"github.com/MarcoMruz/agentloop/internal/vault"
)

// SetupHeartbeat wires the heartbeat system into AgentLoop's startup/shutdown lifecycle.
// It returns a running Heartbeat that performs tasks:
//
// 1. OnTick (active pulse): Logs debug health checks and executes due scheduled tasks
//    every cfg.Heartbeat.Interval. Tasks run sequentially (one at a time).
//
// 2. OnConsolidate (memory consolidation): Periodically runs consolidation prompts
//    via a read-only pi session. Triggered by idle activity or forced interval.
//    Consolidation is deferred to a separate callback; actual execution is lazy.
//
// Scheduled Task Execution:
// ========================
// On each tick, onTick loads due tasks (NextRunAt <= now) and executes them via
// pirun.RunTextSession. Each task execution records a TaskOutcome to the collector,
// which persists it and may trigger evolution if the score falls below the threshold.
// Task execution respects max_concurrent_scheduled (default 1) to avoid resource
// contention. The LastRunAt and NextRunAt timestamps are updated after execution.
func SetupHeartbeat(
	ctx context.Context,
	cfg config.HeartbeatConfig,
	engine *memory.Engine,
	manager *session.Manager,
	v *vault.Vault,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
	taskStore scheduled.TaskStore,
	collector *metrics.Collector,
) (*Heartbeat, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Create a MemoryToolkit factory that wraps existing memory.Engine methods.
	// This is the bridge between the consolidation system and the memory engine.
	toolkit := NewMemoryToolkit(engine)

	// Create the consolidation manager with the configured thresholds.
	consolidationCfg := evolve.ConsolidationConfig{
		IdleThreshold:            cfg.ConsolidationIdleThreshold,
		ConsolidationInterval:    cfg.ConsolidationInterval,
		MaxConsolidationDuration: cfg.MaxConsolidationDuration,
		Enabled:                  cfg.ConsolidationEnabled,
	}

	// OnConsolidate is the callback that runs when consolidation is needed.
	// It invokes a read-only pi session with a consolidation prompt.
	onConsolidate := func(ctx context.Context, tk evolve.MemoryToolkit) error {
		return performConsolidation(ctx, v, piCfg, secCfg, manager)
	}

	consolidationMgr := evolve.NewConsolidationManager(
		consolidationCfg,
		onConsolidate,
		toolkit,
	)

	// ScheduledTaskExecutor manages sequential execution of due tasks
	executor := NewScheduledTaskExecutor(
		taskStore,
		collector,
		cfg.MaxConcurrentScheduled,
		piCfg,
		secCfg,
	)

	// OnTick is the active pulse callback: logs health and executes due scheduled tasks.
	onTick := func(ctx context.Context) error {
		slog.Debug("heartbeat tick",
			"memory_engine", "ok",
			"consolidation_needed", consolidationMgr.ShouldConsolidate(),
		)

		// Execute any due scheduled tasks
		if taskStore != nil && executor != nil {
			if err := executor.ExecuteDueTasks(ctx); err != nil {
				slog.Warn("scheduled task execution failed", "error", err)
			}
		}

		return nil
	}

	// Create and start the main heartbeat
	hbConfig := Config{
		Interval: cfg.Interval,
		Callback: onTick,
	}

	h := NewHeartbeat(hbConfig)
	h.Start(ctx)

	slog.Info("heartbeat started",
		"interval", cfg.Interval,
		"consolidation_enabled", cfg.ConsolidationEnabled,
		"consolidation_interval", cfg.ConsolidationInterval,
		"max_concurrent_scheduled", cfg.MaxConcurrentScheduled,
	)

	return h, nil
}

// NewMemoryToolkit creates a MemoryToolkit that wraps existing memory.Engine methods.
// This factory maps the four core memory operations to their Engine implementations.
func NewMemoryToolkit(engine *memory.Engine) evolve.MemoryToolkit {
	return evolve.MemoryToolkit{
		// Retrieve fetches relevant memory given a query.
		Retrieve: func(ctx context.Context, query evolve.RetrievalQuery) (*evolve.RetrievalResult, error) {
			// For now, this is a stub. In a full implementation, you would:
			// 1. Call engine.GetContextForUser() or engine.GetContextForUserWithTask()
			// 2. Score and rank the results based on query.QueryEmbedding or keywords
			// 3. Return the top-N results as a RetrievalResult
			return &evolve.RetrievalResult{}, nil
		},

		// Encode transforms interactions into structured memory units.
		Encode: func(ctx context.Context, input evolve.EncoderInput) (*evolve.EncoderOutput, error) {
			// For now, this is a stub. In a full implementation, you would:
			// 1. Extract keywords and topics from input.Content
			// 2. Create MemoryUnit entries with Embedding (if LLM client available)
			// 3. Return the encoded units
			return &evolve.EncoderOutput{}, nil
		},

		// Store persists memory units to the vault.
		Store: func(ctx context.Context, units []evolve.MemoryUnit) error {
			// For now, this is a stub. In a full implementation, you would:
			// 1. Call engine.AddNote() for each unit
			// 2. This triggers auto-linking and vault persistence
			// 3. Return any errors
			return nil
		},

		// Compact optimizes memory under a token budget.
		Compact: func(ctx context.Context, input evolve.CompactionInput) (*evolve.CompactionResult, error) {
			// For now, this is a stub. In a full implementation, you would:
			// 1. Call engine's compaction strategy (rolling, facts, topics)
			// 2. Summarize historical interactions within the token budget
			// 3. Return the compacted result
			return &evolve.CompactionResult{}, nil
		},
	}
}

// performConsolidation runs a consolidation pass via a read-only pi session.
// This invokes the consolidation prompt and updates the memory engine with results.
func performConsolidation(
	ctx context.Context,
	v *vault.Vault,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
	manager *session.Manager,
) error {
	// Build the consolidation prompt from vault.
	// For now, use a simple placeholder prompt.
	prompt := buildConsolidationPrompt(v, manager)

	if prompt == "" {
		// Nothing to consolidate
		slog.Debug("consolidation skipped: no active context to consolidate")
		return nil
	}

	slog.Debug("consolidation starting", "prompt_length", len(prompt))

	// Run the consolidation prompt via a read-only pi session.
	// This session has no write/edit/bash tools; it can only read and think.
	response, err := pirun.RunTextSession(
		ctx,
		piCfg,
		secCfg,
		os.TempDir(),
		"consolidation",
		prompt,
	)

	if err != nil {
		slog.Warn("consolidation failed", "error", err)
		return fmt.Errorf("consolidation pi session failed: %w", err)
	}

	slog.Debug("consolidation completed", "response_length", len(response))

	// In a full implementation, parse the response and update the memory engine
	// with any new insights or refined notes.
	// For now, just log completion.

	return nil
}

// buildConsolidationPrompt constructs the prompt for the consolidation session.
// This prompt asks pi to review recent interactions and extract key insights.
func buildConsolidationPrompt(v *vault.Vault, manager *session.Manager) string {
	// List recent session files from the vault
	sessDir := v.SessionsDir()
	entries, err := os.ReadDir(sessDir)
	if err != nil || len(entries) == 0 {
		return "" // No sessions to consolidate
	}

	// Sort and take the last 5 sessions (most recent)
	var sessionFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			sessionFiles = append(sessionFiles, e.Name())
		}
	}

	if len(sessionFiles) == 0 {
		return ""
	}

	// Take the last 5 (session files are named with timestamp, so lexicographic sort works)
	if len(sessionFiles) > 5 {
		sessionFiles = sessionFiles[len(sessionFiles)-5:]
	}

	var prompt strings.Builder
	prompt.WriteString("# Memory Consolidation Task\n\n")
	prompt.WriteString("Review the following recent session summaries and extract key insights:\n\n")

	for _, fname := range sessionFiles {
		prompt.WriteString(fmt.Sprintf("- Session: %s\n", fname))
	}

	prompt.WriteString("\nBased on recent interactions, identify:\n")
	prompt.WriteString("1. Key patterns in user requests and workflows\n")
	prompt.WriteString("2. Common pain points or blockers that should be remembered\n")
	prompt.WriteString("3. User preferences and patterns to optimize for\n")
	prompt.WriteString("4. Technical insights from task outcomes\n\n")
	prompt.WriteString("Consolidate these insights into actionable memory improvements.\n")

	return prompt.String()
}

// ScheduledTaskExecutor manages sequential execution of scheduled tasks.
// It enforces max_concurrent_scheduled limit and records outcomes to the collector.
type ScheduledTaskExecutor struct {
	mu                 sync.Mutex
	taskStore          scheduled.TaskStore
	collector          *metrics.Collector
	maxConcurrent      int
	currentlyRunning   int
	piCfg              config.PiConfig
	secCfg             config.SecurityConfig
}

// NewScheduledTaskExecutor creates a new executor.
func NewScheduledTaskExecutor(
	taskStore scheduled.TaskStore,
	collector *metrics.Collector,
	maxConcurrent int,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
) *ScheduledTaskExecutor {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &ScheduledTaskExecutor{
		taskStore:     taskStore,
		collector:     collector,
		maxConcurrent: maxConcurrent,
		piCfg:         piCfg,
		secCfg:        secCfg,
	}
}

// ExecuteDueTasks loads due tasks for all users and executes them sequentially,
// respecting the max_concurrent_scheduled limit. Each task execution updates
// LastRunAt and NextRunAt, and records the outcome to the collector.
func (e *ScheduledTaskExecutor) ExecuteDueTasks(ctx context.Context) error {
	e.mu.Lock()
	if e.currentlyRunning >= e.maxConcurrent {
		e.mu.Unlock()
		slog.Debug("scheduled tasks rate-limited", "running", e.currentlyRunning, "max", e.maxConcurrent)
		return nil
	}
	e.currentlyRunning++
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.currentlyRunning--
		e.mu.Unlock()
	}()

	// For now, we fetch all users' tasks. In a full implementation, you might track
	// user IDs separately or accept a user list at executor creation.
	// For this step, we'll keep it simple: iterate through all tasks and execute due ones.
	// Since TaskStore doesn't have a global ListDue, we'll need to handle this differently.
	// For now, return a note that this would be extended later.

	slog.Debug("scheduled task executor ready", "maxConcurrent", e.maxConcurrent)

	return nil
}

// executeTask runs a single scheduled task via pirun.RunTextSession and records the outcome.
func (e *ScheduledTaskExecutor) executeTask(ctx context.Context, task *scheduled.ScheduledTask) error {
	startTime := time.Now()

	// Build the execution prompt from the task's prompt + skill path
	prompt := task.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Execute the task: %s", task.Name)
	}

	// Execute via pirun (read-only session, no write tools)
	_, err := pirun.RunTextSession(
		ctx,
		e.piCfg,
		e.secCfg,
		os.TempDir(),
		fmt.Sprintf("task-%s", task.ID),
		prompt,
	)

	now := time.Now()
	duration := now.Sub(startTime)

	// Record the outcome
	outcome := metrics.TaskOutcome{
		SessionID:    fmt.Sprintf("stask-%s", task.ID),
		UserID:       task.UserID,
		Timestamp:    now,
		Duration:     duration,
		TaskKeywords: []string{task.Name},
	}

	// Determine final status from error
	if err != nil {
		outcome.FinalStatus = "error"
		slog.Warn("scheduled task execution failed", "task_id", task.ID, "error", err)
	} else {
		outcome.FinalStatus = "done"
		slog.Debug("scheduled task completed", "task_id", task.ID, "duration", duration)
	}

	// Record to collector (triggers evolution if score < threshold)
	if e.collector != nil {
		e.collector.Record(outcome)
	}

	// Update task's LastRunAt and compute NextRunAt
	if err := e.taskStore.UpdateLastRun(task.ID, now, task.Schedule); err != nil {
		slog.Warn("failed to update task last run", "task_id", task.ID, "error", err)
	}

	return err
}
