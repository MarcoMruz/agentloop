package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/user/agentloop/internal/bridge"
	"github.com/user/agentloop/internal/config"
	"github.com/user/agentloop/internal/hitl"
	"github.com/user/agentloop/internal/vault"
)

type Orchestrator struct {
	cfg   *config.Config
	vault *vault.Vault
}

func New(cfg *config.Config, v *vault.Vault) *Orchestrator {
	return &Orchestrator{cfg: cfg, vault: v}
}

func (o *Orchestrator) RunTask(ctx context.Context, taskDescription string, workDir string) error {
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())

	// Create session note for vault
	note := vault.SessionNote{
		Frontmatter: vault.SessionFrontmatter{
			ID: sessionID, Title: truncate(taskDescription, 60),
			Created: time.Now(), Updated: time.Now(), Status: "in_progress",
			Provider: o.cfg.Pi.Provider, Model: o.cfg.Pi.Model,
		},
		TaskText: taskDescription,
	}

	// Create pi bridge
	b := bridge.New(o.cfg.Pi, o.cfg.Security)

	// Collect transcript and tool usage
	var transcript strings.Builder
	var toolCalls []string

	b.SetEventHandler(func(event bridge.RPCEvent) error {
		switch event.Type {
		case "text":
			transcript.WriteString(event.Content)
			fmt.Print(event.Content) // stream to terminal
		case "tool_use":
			toolCalls = append(toolCalls, event.Name)
			slog.Info("tool call", "tool", event.Name)
		case "tool_result":
			slog.Debug("tool result", "tool", event.Name, "output_len", len(event.Output))
		case "error":
			slog.Error("pi error", "message", event.Message)
			fmt.Printf("\n⚠ Error: %s\n", event.Message)
		case "done":
			slog.Info("prompt completed", "id", event.ID)
		}
		return nil
	})

	// HITL handler: route extension_ui_request through Go-side approval
	b.SetHITLHandler(func(event bridge.RPCEvent) (bool, error) {
		decision, err := hitl.AskUser(
			event.Title, // tool name is in the title from the extension
			nil,
			event.Title,
			o.cfg.HITL.TimeoutSeconds,
		)
		if err != nil {
			return false, err
		}
		note.HITLLog = append(note.HITLLog, vault.HITLEntry{
			Timestamp: time.Now(), ToolName: event.Title, Decision: string(decision),
		})
		return decision == hitl.DecisionApprove, nil
	})

	// Start pi subprocess
	if err := b.Start(ctx, workDir); err != nil {
		return fmt.Errorf("failed to start pi: %w", err)
	}
	defer b.Stop()

	// Send the task
	if err := b.Prompt(ctx, sessionID, taskDescription); err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}

	// Wait for pi to finish
	select {
	case <-b.Done():
		// pi process exited
	case <-ctx.Done():
		// Context cancelled (Ctrl+C)
		b.Abort(sessionID)
		b.Stop()
	}

	// Save session to vault
	note.Transcript = transcript.String()
	note.ToolCalls = unique(toolCalls)
	note.Frontmatter.Updated = time.Now()
	note.Frontmatter.Status = "done"

	if err := o.vault.Write(note); err != nil {
		slog.Warn("failed to save session", "error", err)
	} else {
		slog.Info("session saved", "id", sessionID)
	}

	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

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
