package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MarcoMruz/agentloop/internal/config"
)

// EventHandler is called for each event from pi.
type EventHandler func(event RPCEvent) error

// HITLHandler is called when pi's HITL extension requests approval.
// Returns true to approve, false to deny.
type HITLHandler func(event RPCEvent) (bool, error)

// PiBridge manages the pi subprocess and RPC communication.
type PiBridge struct {
	cfg    config.PiConfig
	secCfg config.SecurityConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	mu     sync.Mutex
	onEvent EventHandler
	onHITL  HITLHandler
	done   chan struct{}
}

// New creates a PiBridge but does not start it. Call Start() separately.
func New(piCfg config.PiConfig, secCfg config.SecurityConfig) *PiBridge {
	return &PiBridge{
		cfg:  piCfg,
		secCfg: secCfg,
		done: make(chan struct{}),
	}
}

// SetEventHandler registers the callback for all pi events.
func (b *PiBridge) SetEventHandler(h EventHandler) { b.onEvent = h }

// SetHITLHandler registers the callback for HITL approval requests.
func (b *PiBridge) SetHITLHandler(h HITLHandler) { b.onHITL = h }

// Start launches the pi subprocess in RPC mode.
func (b *PiBridge) Start(ctx context.Context, workDir string) error {
	binary := b.cfg.Binary
	if binary == "" {
		binary = "pi"
	}

	args := []string{"--mode", "rpc"}
	if b.cfg.Provider != "" {
		args = append(args, "--provider", b.cfg.Provider)
	}
	if b.cfg.Model != "" {
		args = append(args, "--model", b.cfg.Model)
	}

	// Load AgentLoop extensions
	extDir := b.cfg.ExtensionsDir
	if extDir == "" {
		// Auto-detect relative to the agentloop binary
		if exe, err := os.Executable(); err == nil {
			extDir = filepath.Join(filepath.Dir(exe), "extensions")
		}
	}
	if extDir != "" {
		entries, err := os.ReadDir(extDir)
		if err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".ts") || strings.HasSuffix(e.Name(), ".js") {
					args = append(args, "-e", filepath.Join(extDir, e.Name()))
				}
			}
		}
	}

	args = append(args, b.cfg.ExtraArgs...)

	b.cmd = exec.CommandContext(ctx, binary, args...)
	b.cmd.Dir = workDir

	// SECURITY: Build sanitized environment for pi subprocess
	b.cmd.Env = buildSafeEnv(b.secCfg.BlockedEnvPrefixes)

	var err error
	b.stdin, err = b.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	b.stdout, err = b.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	b.stderr, err = b.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := b.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start pi: %w", err)
	}

	// Read stdout events in background
	go b.readEvents()
	// Log stderr
	go b.readStderr()

	slog.Info("pi subprocess started", "pid", b.cmd.Process.Pid, "provider", b.cfg.Provider, "model", b.cfg.Model)
	return nil
}

// SendCommand sends a JSON command to pi via stdin.
func (b *PiBridge) SendCommand(cmd RPCCommand) error {
	return b.sendJSON(cmd)
}

// sendJSON marshals v and writes it as a JSON line to pi's stdin.
func (b *PiBridge) sendJSON(v any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(b.stdin, "%s\n", data)
	return err
}

// Prompt sends a task to pi. Returns immediately; events stream asynchronously.
func (b *PiBridge) Prompt(ctx context.Context, id string, text string) error {
	return b.SendCommand(RPCCommand{Type: "prompt", ID: id, Message: text})
}

// Steer interrupts current work with a new direction.
func (b *PiBridge) Steer(ctx context.Context, id string, text string) error {
	return b.SendCommand(RPCCommand{Type: "steer", Message: text, StreamingBehavior: "steer"})
}

// Abort stops current work.
func (b *PiBridge) Abort(id string) error {
	return b.SendCommand(RPCCommand{Type: "abort"})
}

// Wait blocks until the pi process exits.
func (b *PiBridge) Wait() error {
	return b.cmd.Wait()
}

// Stop gracefully shuts down the pi subprocess.
func (b *PiBridge) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stdin != nil {
		b.stdin.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		return b.cmd.Process.Kill()
	}
	return nil
}

// Done returns a channel that closes when pi exits.
func (b *PiBridge) Done() <-chan struct{} { return b.done }

func (b *PiBridge) readEvents() {
	defer close(b.done)
	scanner := bufio.NewScanner(b.stdout)
	// Increase scanner buffer for large tool outputs
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event RPCEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			slog.Warn("failed to parse pi event", "line", line[:min(len(line), 200)], "error", err)
			continue
		}
		slog.Debug("pi event received", "type", event.Type, "id", event.ID)

		// Handle HITL extension UI requests
		if event.Type == "extension_ui_request" && b.onHITL != nil {
			approved, err := b.onHITL(event)
			if err != nil {
				slog.Warn("HITL handler error", "error", err)
			}
			// Send response directly as ExtensionUIResponse (not wrapped in RPCCommand)
			resp := ExtensionUIResponse{
				Type: "extension_ui_response",
				ID:   event.ID,
			}
			if event.Method == "confirm" {
				resp.Confirmed = &approved
			} else if event.Method == "select" {
				if approved {
					resp.Value = "Allow"
				} else {
					resp.Value = "Block"
					resp.Cancelled = true
				}
			}
			b.sendJSON(resp)
			continue
		}

		// Dispatch to event handler
		if b.onEvent != nil {
			if err := b.onEvent(event); err != nil {
				slog.Warn("event handler error", "type", event.Type, "error", err)
			}
		}
	}
}

func (b *PiBridge) readStderr() {
	scanner := bufio.NewScanner(b.stderr)
	for scanner.Scan() {
		slog.Warn("pi stderr", "line", scanner.Text())
	}
}

// buildSafeEnv creates an environment with sensitive vars stripped.
func buildSafeEnv(blockedPrefixes []string) []string {
	var safe []string
	for _, envVar := range os.Environ() {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToUpper(parts[0])
		blocked := false
		for _, prefix := range blockedPrefixes {
			if strings.HasPrefix(key, strings.ToUpper(prefix)) {
				blocked = true
				break
			}
		}
		if !blocked {
			safe = append(safe, envVar)
		}
	}
	return safe
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
