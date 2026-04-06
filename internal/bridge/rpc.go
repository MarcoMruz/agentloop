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
	"github.com/google/uuid"
)

// EventHandler is called for each event from pi.
type EventHandler func(event RPCEvent) error

// HITLHandler is called when pi's HITL extension requests approval.
// Returns true to approve, false to deny.
type HITLHandler func(event RPCEvent) (bool, error)

// PiBridge manages the pi subprocess and RPC communication.
type PiBridge struct {
	cfg          config.PiConfig
	secCfg       config.SecurityConfig
	hitlCfg      config.HITLConfig
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderr       io.ReadCloser
	mu           sync.Mutex
	onEvent      EventHandler
	onHITL       HITLHandler
	onMemoryTool    MemoryToolHandler
	onSkillTool     SkillToolHandler
	onFeedbackTool  FeedbackToolHandler
	onScheduleTool  ScheduleToolHandler
	done            chan struct{}
	retrievePath  string // per-session temp file for Retrieve_memory results
	skillLoadPath string // per-session temp file for Find_skill result
}

// New creates a PiBridge but does not start it. Call Start() separately.
func New(piCfg config.PiConfig, secCfg config.SecurityConfig, hitlCfg config.HITLConfig) *PiBridge {
	return &PiBridge{
		cfg:     piCfg,
		secCfg:  secCfg,
		hitlCfg: hitlCfg,
		done:    make(chan struct{}),
	}
}

// SetEventHandler registers the callback for all pi events.
func (b *PiBridge) SetEventHandler(h EventHandler) { b.onEvent = h }

// SetHITLHandler registers the callback for HITL approval requests.
func (b *PiBridge) SetHITLHandler(h HITLHandler) { b.onHITL = h }

// SetMemoryToolHandler registers the callback for memory tool interception.
func (b *PiBridge) SetMemoryToolHandler(h MemoryToolHandler) { b.onMemoryTool = h }

// SetSkillToolHandler registers the callback for Find_skill tool interception.
func (b *PiBridge) SetSkillToolHandler(h SkillToolHandler) { b.onSkillTool = h }

// SetFeedbackToolHandler registers the callback for Submit_feedback tool interception.
func (b *PiBridge) SetFeedbackToolHandler(h FeedbackToolHandler) { b.onFeedbackTool = h }

// SetScheduleToolHandler registers the callback for Schedule_task tool interception.
func (b *PiBridge) SetScheduleToolHandler(h ScheduleToolHandler) { b.onScheduleTool = h }

// Start launches the pi subprocess in RPC mode.
func (b *PiBridge) Start(ctx context.Context, workDir string) error {
	// Generate per-session temp file path for Retrieve_memory IPC
	b.retrievePath = filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-retrieve-%s.json", uuid.New().String()[:8]))
	b.skillLoadPath = filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-skill-load-%s.json", uuid.New().String()[:8]))

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
	if b.cfg.NoSkills {
		args = append(args, "--no-skills")
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
	env := buildSafeEnv(b.secCfg.BlockedEnvPrefixes, b.secCfg.Injection, b.hitlCfg)
	env = append(env, "AGENTLOOP_RETRIEVE_PATH="+b.retrievePath)
	env = append(env, "AGENTLOOP_SKILL_LOAD_PATH="+b.skillLoadPath)
	if b.cfg.VaultPath != "" {
		env = append(env, "AGENTLOOP_VAULT_PATH="+b.cfg.VaultPath)
	}
	b.cmd.Env = env

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
	if b.retrievePath != "" {
		_ = os.Remove(b.retrievePath)
	}
	if b.skillLoadPath != "" {
		_ = os.Remove(b.skillLoadPath)
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

		// Skill tool interception: Find_skill is handled by the Go side — not forwarded to OnToolUse
		if event.Type == "tool_execution_start" && event.ToolName == "Find_skill" {
			if b.onSkillTool != nil {
				b.onSkillTool(SkillToolEvent{
					Tool:          "Find_skill",
					Params:        event.Args,
					SkillLoadPath: b.skillLoadPath,
				})
			}
			continue // Do NOT forward to onEvent
		}

		// Feedback tool interception: Submit_feedback is handled by the Go side
		if event.Type == "tool_execution_start" && event.ToolName == "Submit_feedback" {
			if b.onFeedbackTool != nil {
				b.onFeedbackTool(feedbackToolEventFromArgs(event.Args))
			}
			continue // Do NOT forward to onEvent
		}

		// Schedule tool interception: Schedule_task is handled by the Go side
		if event.Type == "tool_execution_start" && event.ToolName == "Schedule_task" {
			if b.onScheduleTool != nil {
				b.onScheduleTool(scheduleToolEventFromArgs(event.Args))
			}
			continue // Do NOT forward to onEvent
		}

		// Dispatch to event handler
		if b.onEvent != nil {
			if err := b.onEvent(event); err != nil {
				slog.Warn("event handler error", "type", event.Type, "error", err)
			}
		}

		// Memory tool interception: fire-and-forget side effect for Add/Update/Delete_memory
		// and synchronous file write for Retrieve_memory (must complete before execute() reads it)
		if event.Type == "tool_execution_start" && isMemoryTool(event.ToolName) && b.onMemoryTool != nil {
			b.onMemoryTool(memoryToolEventFromArgs(event.ToolName, event.Args, b.retrievePath))
		}
	}
}

func isMemoryTool(name string) bool {
	switch name {
	case "Add_memory", "Update_memory", "Delete_memory", "Retrieve_memory":
		return true
	}
	return false
}

func memoryToolOperation(name string) string {
	switch name {
	case "Add_memory":
		return "add"
	case "Update_memory":
		return "update"
	case "Delete_memory":
		return "delete"
	case "Retrieve_memory":
		return "retrieve"
	}
	return ""
}

func memoryToolEventFromArgs(toolName string, args map[string]any, retrievePath string) MemoryToolEvent {
	strVal := func(key string) string {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	intVal := func(key string) int {
		if v, ok := args[key]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			}
		}
		return 5
	}
	strSlice := func(key string) []string {
		if v, ok := args[key]; ok {
			if arr, ok := v.([]any); ok {
				out := make([]string, 0, len(arr))
				for _, x := range arr {
					if s, ok := x.(string); ok {
						out = append(out, s)
					}
				}
				return out
			}
		}
		return nil
	}
	op := memoryToolOperation(toolName)
	return MemoryToolEvent{
		Operation:    op,
		NoteID:       strVal("id"),
		Content:      strVal("content"),
		Keywords:     strSlice("keywords"),
		Tags:         strSlice("tags"),
		Query:        strVal("query"),
		TopK:         intVal("top_k"),
		RetrievePath: retrievePath,
	}
}

func feedbackToolEventFromArgs(args map[string]any) FeedbackToolEvent {
	strVal := func(key string) string {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	return FeedbackToolEvent{
		Text:             strVal("text"),
		Context:          strVal("context"),
		ExpectedBehavior: strVal("expected_behavior"),
		SessionID:        strVal("session_id"),
		UserID:           strVal("user_id"),
	}
}

func scheduleToolEventFromArgs(args map[string]any) ScheduleToolEvent {
	strVal := func(key string) string {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	return ScheduleToolEvent{
		Name:        strVal("name"),
		Schedule:    strVal("schedule"),
		Description: strVal("description"),
		Prompt:      strVal("prompt"),
		SessionID:   strVal("session_id"),
		UserID:      strVal("user_id"),
	}
}

func (b *PiBridge) readStderr() {
	scanner := bufio.NewScanner(b.stderr)
	for scanner.Scan() {
		slog.Warn("pi stderr", "line", scanner.Text())
	}
}

// buildSafeEnv creates an environment with sensitive vars stripped and config vars injected for extensions.
func buildSafeEnv(blockedPrefixes []string, injectionCfg config.InjectionConfig, hitlCfg config.HITLConfig) []string {
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

	// Add injection protection configuration for TypeScript extensions
	if injectionCfg.EnableProtection {
		safe = append(safe, "AGENTLOOP_INJECTION_PROTECTION=true")
		safe = append(safe, "AGENTLOOP_WHITELIST_SOURCES="+strings.Join(injectionCfg.WhitelistSources, ","))
		safe = append(safe, "AGENTLOOP_BLOCKED_KEYWORDS="+strings.Join(injectionCfg.BlockedKeywords, ","))
		safe = append(safe, "AGENTLOOP_REQUIRE_APPROVAL="+strings.Join(injectionCfg.RequireApproval, ","))
		safe = append(safe, fmt.Sprintf("AGENTLOOP_MAX_CONTENT_LENGTH=%d", injectionCfg.MaxContentLength))
		safe = append(safe, fmt.Sprintf("AGENTLOOP_DETECTION_THRESHOLD=%.2f", injectionCfg.DetectionThreshold))
		safe = append(safe, "AGENTLOOP_APPROVAL_TIER="+injectionCfg.ApprovalTier)
		if injectionCfg.SanitizeMemory {
			safe = append(safe, "AGENTLOOP_SANITIZE_MEMORY=true")
		}
	} else {
		safe = append(safe, "AGENTLOOP_INJECTION_PROTECTION=false")
	}

	// Inject HITL force keywords for the force-hitl extension
	if len(hitlCfg.ForceHITLKeywords) > 0 {
		safe = append(safe, "AGENTLOOP_HITL_FORCE_KEYWORDS="+strings.Join(hitlCfg.ForceHITLKeywords, ","))
	}

	return safe
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
