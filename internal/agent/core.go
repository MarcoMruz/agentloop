package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/user/agentloop/internal/bridge"
	"github.com/user/agentloop/internal/config"
)

type RunStats struct {
	Tokens    int
	ToolCalls int
	Duration  time.Duration
}

type RunResult struct {
	Output    string
	Error     string
	ToolsUsed []string
	Stats     RunStats
}

type Callbacks struct {
	OnText        func(content string)
	OnToolUse     func(name string, input map[string]any)
	OnToolResult  func(name string, output string, success bool)
	OnHITLRequest func(requestId string, toolName string, details string)
	OnDone        func(output string, stats RunStats)
	OnError       func(msg string)
}

// SessionInterface is the subset of Session needed by the agent core.
type SessionInterface interface {
	WaitHITL(requestId string, timeout time.Duration) (string, error)
	AbortCh() <-chan struct{}
	SteerCh() <-chan string
}

type Core struct {
	piCfg  config.PiConfig
	secCfg config.SecurityConfig
	cb     Callbacks
}

func New(piCfg config.PiConfig, secCfg config.SecurityConfig, cb Callbacks) *Core {
	return &Core{piCfg: piCfg, secCfg: secCfg, cb: cb}
}

func (c *Core) Run(ctx context.Context, memoryContext string, task string, sess SessionInterface) RunResult {
	start := time.Now()
	var output strings.Builder
	var toolsUsed []string

	// Build full prompt with memory prefix
	fullPrompt := c.buildPrompt(memoryContext, task)

	// Create pi bridge
	b := bridge.New(c.piCfg, c.secCfg)

	doneCh := make(chan struct{})

	b.SetEventHandler(func(event bridge.RPCEvent) error {
		switch event.Type {
		case "text":
			output.WriteString(event.Content)
			if c.cb.OnText != nil { c.cb.OnText(event.Content) }

		case "tool_use":
			toolsUsed = append(toolsUsed, event.Name)
			if c.cb.OnToolUse != nil { c.cb.OnToolUse(event.Name, event.Input) }

		case "tool_result":
			success := !strings.Contains(strings.ToLower(event.Output), "error")
			if c.cb.OnToolResult != nil { c.cb.OnToolResult(event.Name, event.Output, success) }

		case "done":
			close(doneCh)

		case "error":
			if c.cb.OnError != nil { c.cb.OnError(event.Message) }
			close(doneCh)
		}
		return nil
	})

	// HITL handler: route through session's HITL resolution
	b.SetHITLHandler(func(event bridge.RPCEvent) (bool, error) {
		requestId := uuid.New().String()[:8]
		toolName := event.Title
		details := fmt.Sprintf("%s: %s", event.Method, event.Title)

		// Notify client (Slack/CLI) about HITL request
		if c.cb.OnHITLRequest != nil {
			c.cb.OnHITLRequest(requestId, toolName, details)
		}

		// Wait for resolution from client (blocks until approve/deny/abort)
		decision, err := sess.WaitHITL(requestId, 5*time.Minute)
		if err != nil {
			slog.Warn("HITL wait error", "error", err)
			return false, err
		}

		switch decision {
		case "approve":
			return true, nil
		case "abort":
			return false, fmt.Errorf("session aborted by user")
		default: // "deny" or timeout
			return false, nil
		}
	})

	// Start pi
	workDir := "" // session workDir passed via context
	if err := b.Start(ctx, workDir); err != nil {
		return RunResult{Error: fmt.Sprintf("failed to start pi: %v", err)}
	}
	defer b.Stop()

	// Send prompt
	if err := b.Prompt(ctx, "task", fullPrompt); err != nil {
		return RunResult{Error: fmt.Sprintf("failed to send prompt: %v", err)}
	}

	// Wait for completion, abort, or steer
	for {
		select {
		case <-doneCh:
			stats := RunStats{
				Tokens:    estimateTokens(output.String()),
				ToolCalls: len(toolsUsed),
				Duration:  time.Since(start),
			}
			result := RunResult{
				Output:    output.String(),
				ToolsUsed: unique(toolsUsed),
				Stats:     stats,
			}
			if c.cb.OnDone != nil { c.cb.OnDone(result.Output, stats) }
			return result

		case <-sess.AbortCh():
			b.Abort("task")
			return RunResult{Output: output.String(), Error: "aborted by user", ToolsUsed: unique(toolsUsed)}

		case steerText := <-sess.SteerCh():
			b.Steer(ctx, "steer", steerText)

		case <-ctx.Done():
			b.Abort("task")
			return RunResult{Output: output.String(), Error: "context cancelled", ToolsUsed: unique(toolsUsed)}
		}
	}
}

// buildPrompt assembles the full prompt with memory context as a stable prefix.
//
// Structure (optimized for Anthropic prompt caching):
//   1. <memory> block (user profile + compacted history) — STABLE PREFIX
//   2. Task text — DYNAMIC
//
// The memory block changes slowly (profile rarely, history compacted daily),
// so Anthropic's prompt cache can reuse the prefix across calls.
func (c *Core) buildPrompt(memoryContext string, task string) string {
	var parts []string

	if memoryContext != "" {
		parts = append(parts, fmt.Sprintf("<memory>\n%s\n</memory>", memoryContext))
	}

	parts = append(parts, task)
	return strings.Join(parts, "\n\n")
}

func estimateTokens(text string) int { return len(text) / 4 }

func unique(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] { seen[s] = true; out = append(out, s) }
	}
	return out
}

