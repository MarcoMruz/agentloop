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
	RequestId        string
	ToolName         string
	Method           string
	Title            string
	Command          string         // Full command or description
	WorkDir          string         // Current working directory
	Rule             string         // Security rule that triggered this

	// Enriched context fields for Slack/CLI display
	ToolCategory     string         // "file", "bash", "network", "process", "other"
	FilePath         string         // Specific file/dir path (for file tools)
	WhitelistedPaths []string       // Allowed paths from security config
	StructuredInput  map[string]any // Parsed tool input key/values
	RiskLevel        string         // "low", "medium", "high"
	Reason           string         // Human-readable explanation of why blocked
}

type Callbacks struct {
	OnText        func(content string)
	OnToolUse     func(name string, input map[string]any)
	OnToolResult  func(name string, output string, success bool)
	OnHITLRequest func(details HITLRequestDetails)
	OnDone        func(output string, stats RunStats)
	OnError       func(msg string)
	OnMemoryTool    bridge.MemoryToolHandler
	OnSkillTool     bridge.SkillToolHandler
	OnFeedbackTool  bridge.FeedbackToolHandler
}

// SessionInterface is the subset of Session needed by the agent core.
type SessionInterface interface {
	WaitHITL(requestId string, timeout time.Duration) (string, error)
	AbortCh() <-chan struct{}
	SteerCh() <-chan string
	// GetConversationContextID returns the thread/context key for memory scoping.
	// Returns empty string for CLI sessions.
	GetConversationContextID() string
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

// parseHITLDetails extracts command and rule information from HITL event details.
// It parses both the title and the message body from the extension UI request.
func parseHITLDetails(title, message, method string) (command, rule string) {
	combined := title + "\n" + message

	// Extract command from common patterns
	if strings.Contains(combined, "Full command:") {
		parts := strings.Split(combined, "Full command:")
		if len(parts) > 1 {
			commandPart := strings.Split(parts[1], "\n")[0]
			command = strings.TrimSpace(commandPart)
		}
	}

	// Extract rule from title patterns
	lowerTitle := strings.ToLower(title)
	switch {
	case strings.Contains(lowerTitle, "dangerous"):
		if q := extractQuoted(combined); q != "" {
			rule = "Dangerous Pattern: " + q
		}
	case strings.Contains(lowerTitle, "environment"):
		if q := extractQuoted(combined); q != "" {
			rule = "Environment Access: " + q
		}
	case strings.Contains(lowerTitle, "docker"):
		rule = "Docker Command Approval"
	case strings.Contains(lowerTitle, "file path") || strings.Contains(lowerTitle, "path access"):
		rule = "Path Restriction"
	}

	if command == "" {
		command = title
	}
	if rule == "" {
		rule = method
	}

	return command, rule
}

// classifyToolCategory determines the tool category from the extension title.
func classifyToolCategory(title string) string {
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, "file") || strings.Contains(lower, "path"):
		return "file"
	case strings.Contains(lower, "docker") || strings.Contains(lower, "process"):
		return "process"
	case strings.Contains(lower, "curl") || strings.Contains(lower, "wget") || strings.Contains(lower, "network") || strings.Contains(lower, "fetch"):
		return "network"
	case strings.Contains(lower, "command") || strings.Contains(lower, "dangerous") || strings.Contains(lower, "environment"):
		return "bash"
	default:
		return "other"
	}
}

// classifyRiskLevel determines the risk level from the title and rule.
func classifyRiskLevel(title, rule string) string {
	lower := strings.ToLower(title + " " + rule)
	switch {
	case strings.Contains(lower, "dangerous") || strings.Contains(lower, "rm -rf") ||
		strings.Contains(lower, "sudo") || strings.Contains(lower, "mkfs"):
		return "high"
	case strings.Contains(lower, "docker") || strings.Contains(lower, "environment") ||
		strings.Contains(lower, "outside") || strings.Contains(lower, "path restriction"):
		return "medium"
	default:
		return "low"
	}
}

// extractFilePath pulls a file path from the extension message body.
func extractFilePath(message string) string {
	// Look for "Path: /some/path" pattern used by security-policy.ts
	if idx := strings.Index(message, "Path: "); idx != -1 {
		rest := message[idx+6:]
		end := strings.IndexAny(rest, "\n\r")
		if end == -1 {
			end = len(rest)
		}
		return strings.TrimSpace(rest[:end])
	}
	return ""
}

// buildStructuredInput constructs a key/value map from the extension message.
func buildStructuredInput(title, message, filePath string) map[string]any {
	input := map[string]any{}

	if filePath != "" {
		input["path"] = filePath
	}

	// Extract "Full command: ..." from message
	if strings.Contains(message, "Full command:") {
		parts := strings.Split(message, "Full command:")
		if len(parts) > 1 {
			cmd := strings.TrimSpace(strings.Split(parts[1], "\n")[0])
			if cmd != "" {
				input["command"] = cmd
			}
		}
	}

	// Extract "Tool: ..." from message
	if strings.Contains(message, "Tool: ") {
		parts := strings.Split(message, "Tool: ")
		if len(parts) > 1 {
			tool := strings.TrimSpace(strings.Split(parts[1], "\n")[0])
			if tool != "" {
				input["tool"] = tool
			}
		}
	}

	if len(input) == 0 {
		return nil
	}
	return input
}

// buildReason generates a human-readable explanation for the HITL block.
func buildReason(title, category, rule string) string {
	lower := strings.ToLower(title)
	switch {
	case category == "file" && strings.Contains(lower, "outside"):
		return "The requested path is outside all configured safe directories."
	case category == "file":
		return "File operation requires approval — path is not in the whitelist."
	case strings.Contains(lower, "dangerous"):
		return "Command contains a pattern classified as dangerous."
	case strings.Contains(lower, "environment"):
		return "Command attempts to access sensitive environment variables."
	case strings.Contains(lower, "docker"):
		return "Docker command requires explicit approval."
	default:
		return "This operation requires human approval before proceeding."
	}
}

// extractQuoted returns the first double-quoted substring in s.
func extractQuoted(s string) string {
	start := strings.Index(s, `"`)
	if start == -1 {
		return ""
	}
	end := strings.Index(s[start+1:], `"`)
	if end == -1 {
		return ""
	}
	return s[start+1 : start+1+end]
}

// enrichHITLDetails populates the enriched fields on HITLRequestDetails using
// the extension UI event and the server's security config.
func (c *Core) enrichHITLDetails(details *HITLRequestDetails, event bridge.RPCEvent) {
	details.ToolCategory = classifyToolCategory(event.Title)
	details.RiskLevel = classifyRiskLevel(event.Title, details.Rule)
	details.Reason = buildReason(event.Title, details.ToolCategory, details.Rule)

	// Extract file path from extension message body
	uiMsg := event.UIMessageString()
	details.FilePath = extractFilePath(uiMsg)

	// For file tools, include whitelisted paths from security config
	if details.ToolCategory == "file" {
		details.WhitelistedPaths = c.secCfg.AllowedPaths
	}

	// Build structured input from available data
	details.StructuredInput = buildStructuredInput(event.Title, uiMsg, details.FilePath)
}

func (c *Core) Run(ctx context.Context, userId string, task string, workDir string, sess SessionInterface) RunResult {
	start := time.Now()
	var output strings.Builder
	var toolsUsed []string

	slog.Debug("Core.Run starting", "task_len", len(task), "userId", userId)

	// Build the full prompt: memory context + task
	conversationContextID := sess.GetConversationContextID()
	fullPrompt, err := c.pb.Build(userId, task, conversationContextID)
	if err != nil {
		slog.Warn("Core.Run: prompt build error, falling back to raw task", "err", err)
		fullPrompt = task
	}
	slog.Debug("Core.Run after buildPrompt", "prompt_len", len(fullPrompt))

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

		// Extract command details from both title and message body
		command, rule := parseHITLDetails(event.Title, event.UIMessageString(), event.Method)

		// Build detailed HITL request with enriched context
		details := HITLRequestDetails{
			RequestId: requestId,
			ToolName:  event.Title,
			Method:    event.Method,
			Title:     event.Title,
			Command:   command,
			WorkDir:   workDir,
			Rule:      rule,
		}
		c.enrichHITLDetails(&details, event)

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

	if c.cb.OnMemoryTool != nil {
		b.SetMemoryToolHandler(c.cb.OnMemoryTool)
	}

	if c.cb.OnSkillTool != nil {
		b.SetSkillToolHandler(c.cb.OnSkillTool)
	}

	if c.cb.OnFeedbackTool != nil {
		b.SetFeedbackToolHandler(c.cb.OnFeedbackTool)
	}

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
