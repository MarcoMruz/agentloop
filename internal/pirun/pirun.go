package pirun

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/MarcoMruz/agentloop/internal/bridge"
	"github.com/MarcoMruz/agentloop/internal/config"
)

// RunTextSession starts a pi subprocess, sends a single prompt, collects the
// full text response, and returns it. The subprocess exits after responding.
// Pass os.TempDir() as workDir when no real working directory is needed.
func RunTextSession(
	ctx context.Context,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
	workDir string,
	promptID string,
	prompt string,
) (string, error) {
	if workDir == "" {
		workDir = os.TempDir()
	}

	b := bridge.New(piCfg, secCfg, config.HITLConfig{})

	var response strings.Builder

	// doneCh closes on agent_end; pi stays alive in --mode rpc so we cannot
	// rely on b.Done() (process exit) to signal completion.
	doneCh := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(doneCh) }) }

	b.SetEventHandler(func(event bridge.RPCEvent) error {
		switch event.Type {
		case "message_update":
			if event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == "text_delta" {
				response.WriteString(event.AssistantMessageEvent.Delta)
			}
		case "agent_end":
			closeDone()
		case "auto_retry_end":
			if !event.Success {
				closeDone()
			}
		}
		return nil
	})

	// Handle HITL in read-only sessions (planner, judge, skill agent, meta agent).
	// Auto-approve read-only bash exploration (find, grep, git log, curl GET, etc.)
	// so agents can gather context. Deny patterns come from security.readonly_sessions
	// in agentloop.yaml — anything matching those patterns is auto-denied.
	b.SetHITLHandler(func(event bridge.RPCEvent) (bool, error) {
		approve := isReadOnlyHITL(event, secCfg)
		slog.Debug("RunTextSession HITL", "title", event.Title, "command", extractHITLCommand(event.UIMessageString()), "approve", approve)
		return approve, nil
	})

	if err := b.Start(ctx, workDir); err != nil {
		return "", fmt.Errorf("start pi: %w", err)
	}
	defer b.Stop()

	if err := b.Prompt(ctx, promptID, prompt); err != nil {
		return "", fmt.Errorf("prompt pi: %w", err)
	}

	select {
	case <-doneCh:
	case <-b.Done(): // pi process exited unexpectedly
	case <-ctx.Done():
		return response.String(), ctx.Err()
	}
	return response.String(), nil
}

// isReadOnlyHITL returns true when the HITL request is for a read-only operation
// that should be auto-approved in internal sessions (planner, judge, skill agent).
// It matches the bash command from the HITL message against secCfg.ReadonlySessions.DenyPatterns;
// if any pattern matches the request is denied, otherwise approved.
func isReadOnlyHITL(event bridge.RPCEvent, secCfg config.SecurityConfig) bool {
	title := strings.ToLower(event.Title)

	// File write/edit tool operations are never read-only.
	if strings.Contains(title, "file operation") || strings.Contains(title, "path access") {
		return false
	}

	// Extract the bash command from the HITL message body.
	// Extension format: "Command requires approval:\n\n<command>\n\n..."
	command := extractHITLCommand(event.UIMessageString())
	if command == "" {
		return false // can't determine intent — deny to be safe
	}

	for _, pattern := range secCfg.ReadonlySessions.DenyPatterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return false
		}
	}
	return true
}

// extractHITLCommand pulls the command out of the extension's confirm() message body.
func extractHITLCommand(msg string) string {
	const marker = "Command requires approval:\n\n"
	idx := strings.Index(msg, marker)
	if idx == -1 {
		return ""
	}
	rest := msg[idx+len(marker):]
	end := strings.Index(rest, "\n\n")
	if end == -1 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}
