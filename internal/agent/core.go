package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MarcoMruz/agentloop/internal/bridge"
	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/google/uuid"
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

type HITLRequestDetails struct {
	RequestId   string
	ToolName    string
	Method      string
	Title       string
	Command     string  // Full command or description
	WorkDir     string  // Current working directory
	Rule        string  // Security rule that triggered this
}

type Callbacks struct {
	OnText        func(content string)
	OnToolUse     func(name string, input map[string]any)
	OnToolResult  func(name string, output string, success bool)
	OnHITLRequest func(details HITLRequestDetails)
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
	piCfg   config.PiConfig
	secCfg  config.SecurityConfig
	hitlCfg config.HITLConfig
	pb      *PromptBuilder
	cb      Callbacks
}

func New(piCfg config.PiConfig, secCfg config.SecurityConfig, hitlCfg config.HITLConfig, pb *PromptBuilder, cb Callbacks) *Core {
	return &Core{piCfg: piCfg, secCfg: secCfg, hitlCfg: hitlCfg, pb: pb, cb: cb}
}

// parseHITLDetails extracts command and rule information from HITL event details
func parseHITLDetails(title, method string) (command, rule string) {
	// Extract command from common patterns in title
	if strings.Contains(title, "Full command:") {
		parts := strings.Split(title, "Full command:")
		if len(parts) > 1 {
			commandPart := strings.Split(parts[1], "\n")[0]
			command = strings.TrimSpace(commandPart)
		}
	}
	
	// Extract rule from title patterns
	if strings.Contains(title, "dangerous pattern") {
		if start := strings.Index(title, `"`); start != -1 {
			if end := strings.Index(title[start+1:], `"`); end != -1 {
				rule = "Dangerous Pattern: " + title[start+1:start+1+end]
			}
		}
	} else if strings.Contains(title, "environment variables") {
		if start := strings.Index(title, `"`); start != -1 {
			if end := strings.Index(title[start+1:], `"`); end != -1 {
				rule = "Environment Access: " + title[start+1:start+1+end]
			}
		}
	} else if strings.Contains(title, "Docker command") {
		rule = "Docker Command Approval"
	} else if strings.Contains(title, "outside allowed paths") {
		rule = "Path Restriction"
	}

	// Fallback to basic method/title if no specific parsing worked
	if command == "" {
		command = title
	}
	if rule == "" {
		rule = method
	}

	return command, rule
}

func (c *Core) Run(ctx context.Context, userId string, task string, workDir string, sess SessionInterface) RunResult {
	start := time.Now()
	var output strings.Builder
	var toolsUsed []string

	slog.Debug("Core.Run starting", "task_len", len(task), "userId", userId)

	// Build the full prompt: memory context + skills + task
	skillNames := c.pb.DetectSkills(task)
	fullPrompt, err := c.pb.Build(userId, task, skillNames)
	if err != nil {
		slog.Warn("Core.Run: prompt build error, falling back to raw task", "err", err)
		fullPrompt = task
	}
	slog.Debug("Core.Run after buildPrompt", "prompt_len", len(fullPrompt), "skills", skillNames)

	// Create pi bridge
	b := bridge.New(c.piCfg, c.secCfg, c.hitlCfg)

	doneCh := make(chan struct{})
	var doneOnce sync.Once
	closeDone := func() { doneOnce.Do(func() { close(doneCh) }) }

	b.SetEventHandler(func(event bridge.RPCEvent) error {
		switch event.Type {
		case "message_update":
			if event.AssistantMessageEvent == nil {
				break
			}
			switch event.AssistantMessageEvent.Type {
			case "text_delta":
				delta := event.AssistantMessageEvent.Delta
				output.WriteString(delta)
				if c.cb.OnText != nil {
					c.cb.OnText(delta)
				}
			case "error":
				if c.cb.OnError != nil {
					c.cb.OnError("LLM streaming error")
				}
				closeDone()
			}

		case "tool_execution_start":
			toolsUsed = append(toolsUsed, event.ToolName)
			if c.cb.OnToolUse != nil {
				c.cb.OnToolUse(event.ToolName, event.Args)
			}

		case "tool_execution_end":
			if c.cb.OnToolResult != nil {
				var outText string
				if event.Result != nil {
					for _, block := range event.Result.Content {
						if block.Type == "text" {
							outText += block.Text
						}
					}
				}
				c.cb.OnToolResult(event.ToolName, outText, !event.IsError)
			}

		case "agent_end":
			closeDone()

		case "auto_retry_end":
			if !event.Success {
				errMsg := event.FinalError
				if errMsg == "" {
					errMsg = "agent failed after retries"
				}
				if c.cb.OnError != nil {
					c.cb.OnError(errMsg)
				}
				closeDone()
			}
		}
		return nil
	})

	// HITL handler: route through session's HITL resolution
	b.SetHITLHandler(func(event bridge.RPCEvent) (bool, error) {
		requestId := uuid.New().String()[:8]
		
		// Extract command details from the title/method for enhanced display
		command, rule := parseHITLDetails(event.Title, event.Method)

		// Build detailed HITL request
		details := HITLRequestDetails{
			RequestId: requestId,
			ToolName:  event.Title,
			Method:    event.Method, 
			Title:     event.Title,
			Command:   command,
			WorkDir:   workDir,
			Rule:      rule,
		}

		// Notify client (Slack/CLI) about HITL request
		if c.cb.OnHITLRequest != nil {
			c.cb.OnHITLRequest(details)
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
	if err := b.Start(ctx, workDir); err != nil {
		errMsg := fmt.Sprintf("failed to start pi: %v", err)
		if c.cb.OnError != nil {
			c.cb.OnError(errMsg)
		}
		return RunResult{Error: errMsg}
	}
	defer b.Stop()

	// Send prompt
	if err := b.Prompt(ctx, "task", fullPrompt); err != nil {
		errMsg := fmt.Sprintf("failed to send prompt: %v", err)
		if c.cb.OnError != nil {
			c.cb.OnError(errMsg)
		}
		return RunResult{Error: errMsg}
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
			if c.cb.OnDone != nil {
				c.cb.OnDone(result.Output, stats)
			}
			return result

		case <-b.Done():
			// pi process exited. Check if we already received a "done" event
			// (race: pi exits immediately after sending "done").
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
				if c.cb.OnDone != nil {
					c.cb.OnDone(result.Output, stats)
				}
				return result
			default:
				errMsg := "pi process exited unexpectedly"
				if c.cb.OnError != nil {
					c.cb.OnError(errMsg)
				}
				return RunResult{Output: output.String(), Error: errMsg, ToolsUsed: unique(toolsUsed)}
			}

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

func estimateTokens(text string) int { return len(text) / 4 }

func unique(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
