package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/MarcoMruz/agentloop/internal/agent"
	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/skills"
	"github.com/MarcoMruz/agentloop/internal/vault"
)

type Broadcaster interface {
	Broadcast(sessionId string, method string, params any)
}

type StartRequest struct {
	UserID       string
	Text         string
	WorkDir      string
	Source       string // "cli", "slack", etc.
	Broadcaster  Broadcaster
	HITLNotifier Broadcaster // same interface, routes hitl_request events

	// Slack thread metadata. When both ChannelID and ThreadID are set,
	// ComputeConversationContextID returns a stable key scoping memory to this thread.
	ChannelID string
	ThreadID  string
}

// ComputeConversationContextID returns a stable context key for memory scoping.
// Returns empty string when not applicable (CLI, missing fields, etc.).
func (r StartRequest) ComputeConversationContextID() string {
	if r.ChannelID == "" || r.ThreadID == "" {
		return ""
	}
	return r.ChannelID + ":" + r.ThreadID
}

type Manager struct {
	cfg      config.SessionConfig
	piCfg    config.PiConfig
	secCfg   config.SecurityConfig
	hitlCfg  config.HITLConfig
	vault    *vault.Vault
	memory   *memory.Engine
	skills   *skills.Registry
	sessions map[string]*Session
	userMap  map[string][]string // userId → active sessionIds
	mu       sync.RWMutex
}

func NewManager(cfg *config.Config, v *vault.Vault, mem *memory.Engine, sk *skills.Registry) *Manager {
	return &Manager{
		cfg:      cfg.Sessions,
		piCfg:    cfg.Pi,
		secCfg:   cfg.Security,
		hitlCfg:  cfg.HITL,
		vault:    v,
		memory:   mem,
		skills:   sk,
		sessions: make(map[string]*Session),
		userMap:  make(map[string][]string),
	}
}

func (m *Manager) StartSession(ctx context.Context, req StartRequest) (*Session, error) {
	m.mu.Lock()

	// Enforce limits
	if len(m.sessions) >= m.cfg.MaxConcurrent {
		if !m.cfg.EvictLRU {
			m.mu.Unlock()
			return nil, fmt.Errorf("max concurrent sessions (%d) reached", m.cfg.MaxConcurrent)
		}
		evictedID := m.evictOldestLRU()
		slog.Info("evicted LRU session to make room", "evicted", evictedID)
	}
	userSessions := m.userMap[req.UserID]
	if len(userSessions) >= m.cfg.MaxPerUser {
		m.mu.Unlock()
		return nil, fmt.Errorf("max sessions per user (%d) reached — use task.abort first", m.cfg.MaxPerUser)
	}

	sess := NewSession(req.UserID, req.Text, req.WorkDir, req.Source, req.ChannelID, req.ThreadID)
	m.sessions[sess.ID] = sess
	m.userMap[req.UserID] = append(m.userMap[req.UserID], sess.ID)
	m.mu.Unlock()

	// Run agent in background
	go func() {
		defer m.cleanupSession(sess)

		pb := agent.NewPromptBuilder(m.memory, m.skills)
		agentCore := agent.New(m.piCfg, m.secCfg, m.hitlCfg, pb, agent.Callbacks{
			OnText: func(content string) {
				sess.Touch()
				req.Broadcaster.Broadcast(sess.ID, "event.text", map[string]any{
					"sessionId": sess.ID, "content": content,
				})
			},
			OnToolUse: func(name string, input map[string]any) {
				req.Broadcaster.Broadcast(sess.ID, "event.tool_use", map[string]any{
					"sessionId": sess.ID, "toolName": name, "input": input,
				})
			},
			OnToolResult: func(name string, output string, success bool) {
				req.Broadcaster.Broadcast(sess.ID, "event.tool_result", map[string]any{
					"sessionId": sess.ID, "toolName": name, "output": truncate(output, 2000), "success": success,
				})
			},
			OnHITLRequest: func(details agent.HITLRequestDetails) {
				sess.SetPendingHITL(details.RequestId)
				req.HITLNotifier.Broadcast(sess.ID, "event.hitl_request", map[string]any{
					"sessionId": sess.ID, "requestId": details.RequestId,
					"toolName": details.ToolName, "details": details.Title,
					"command": details.Command, "workDir": details.WorkDir,
					"rule": details.Rule, "method": details.Method,
					"options": []string{"approve", "deny", "abort"},
				})
			},
			OnDone: func(output string, stats agent.RunStats) {
				req.Broadcaster.Broadcast(sess.ID, "event.done", map[string]any{
					"sessionId": sess.ID, "output": output,
					"stats": map[string]any{
						"tokens": stats.Tokens, "toolCalls": stats.ToolCalls,
						"duration": stats.Duration.String(),
					},
				})
			},
			OnError: func(msg string) {
				req.Broadcaster.Broadcast(sess.ID, "event.error", map[string]any{
					"sessionId": sess.ID, "message": msg,
				})
			},
		})

		result := agentCore.Run(ctx, req.UserID, req.Text, req.WorkDir, sess)
		sess.SetResult(result)

		// Persist to vault
		if err := m.vault.WriteSession(sess.ToVaultNote(result)); err != nil {
			slog.Warn("vault write failed", "session", sess.ID, "error", err)
		} else {
			req.Broadcaster.Broadcast(sess.ID, "event.session_saved", map[string]any{
				"sessionId": sess.ID,
			})
		}

		// Update memory with this interaction
		m.memory.RecordInteraction(req.UserID, req.Text, result.Output, result.ToolsUsed, sess.ConversationContextID)
	}()

	return sess, nil
}

func (m *Manager) Steer(sessionId string, text string) error {
	m.mu.RLock()
	sess := m.sessions[sessionId]
	m.mu.RUnlock()
	if sess == nil { return fmt.Errorf("session %q not found", sessionId) }
	return sess.Steer(text)
}

func (m *Manager) Abort(sessionId string) error {
	m.mu.RLock()
	sess := m.sessions[sessionId]
	m.mu.RUnlock()
	if sess == nil { return fmt.Errorf("session %q not found", sessionId) }
	sess.Abort()
	return nil
}

func (m *Manager) ResolveHITL(sessionId string, requestId string, decision string) error {
	m.mu.RLock()
	sess := m.sessions[sessionId]
	m.mu.RUnlock()
	if sess == nil { return fmt.Errorf("session %q not found", sessionId) }
	return sess.ResolveHITL(requestId, decision)
}

func (m *Manager) List(userId string, status string) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []SessionInfo
	for _, sess := range m.sessions {
		if userId != "" && sess.UserID != userId { continue }
		if status != "" && string(sess.State) != status { continue }
		out = append(out, sess.Info())
	}
	return out
}

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sess := range m.sessions {
		sess.Abort()
	}
}

func (m *Manager) cleanupSession(sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sess.ID)
	ids := m.userMap[sess.UserID]
	for i, id := range ids {
		if id == sess.ID {
			m.userMap[sess.UserID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(m.userMap[sess.UserID]) == 0 {
		delete(m.userMap, sess.UserID)
	}
}

// evictOldestLRU aborts and removes the session with the oldest LastActivity.
// Must be called with m.mu write-locked.
func (m *Manager) evictOldestLRU() string {
	var oldest *Session
	for _, sess := range m.sessions {
		if oldest == nil || sess.GetLastActivity().Before(oldest.GetLastActivity()) {
			oldest = sess
		}
	}
	if oldest == nil {
		return ""
	}
	oldest.Abort()
	// Remove from maps immediately so cleanupSession (called by the goroutine
	// defer) becomes a no-op rather than re-cleaning already-absent entries.
	delete(m.sessions, oldest.ID)
	ids := m.userMap[oldest.UserID]
	for i, id := range ids {
		if id == oldest.ID {
			m.userMap[oldest.UserID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(m.userMap[oldest.UserID]) == 0 {
		delete(m.userMap, oldest.UserID)
	}
	return oldest.ID
}

func truncate(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n] + "...[truncated]"
}
