package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MarcoMruz/agentloop/internal/heartbeat/scheduled"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
	"github.com/MarcoMruz/agentloop/internal/session"
	"github.com/MarcoMruz/agentloop/internal/skills"
)

type Handler struct {
	sessions  *session.Manager
	memory    *memory.Engine
	server    *Server // back-reference for broadcasting
	taskStore scheduled.TaskStore
	skills    *skills.Registry
}

func NewHandler(sm *session.Manager, mem *memory.Engine) *Handler {
	return &Handler{sessions: sm, memory: mem}
}

// SetTaskStore sets the task store for schedule operations.
func (h *Handler) SetTaskStore(ts scheduled.TaskStore) {
	h.taskStore = ts
}

// SetSkillRegistry sets the skills registry for skill injection.
func (h *Handler) SetSkillRegistry(sk *skills.Registry) {
	h.skills = sk
}

func (h *Handler) SetServer(s *Server) { h.server = s }

func (h *Handler) Handle(ctx context.Context, client *Client, req JSONRPCRequest) (any, *RPCError) {
	switch req.Method {

	case "task.start":
		var p struct {
			UserID   string `json:"userId"`
			Text     string `json:"text"`
			WorkDir  string `json:"workDir"`
			Source   string `json:"source"`
			ThreadID  string `json:"thread_id"`
			ChannelID string `json:"channel_id"`
		}
		json.Unmarshal(req.Params, &p)
		if p.UserID == "" || p.Text == "" {
			return nil, &RPCError{Code: -32602, Message: "userId and text required"}
		}

		// Convention-based feedback routing: Slack bridges (and other clients) that
		// always send task.start can prefix their message with "feedback:" or "/feedback "
		// to route to feedback.submit logic instead of starting a new agent session.
		if feedbackText, ok := extractFeedbackPrefix(p.Text); ok {
			fb := metrics.UserFeedback{
				UserID:       p.UserID,
				FeedbackText: feedbackText,
				Timestamp:    time.Now(),
			}
			if err := h.sessions.SubmitFeedback(fb); err != nil {
				return nil, &RPCError{Code: -32000, Message: err.Error()}
			}
			return map[string]any{"ok": true, "routed": "feedback"}, nil
		}

		// Convention-based schedule routing: Slack bridges (and other clients) that
		// always send task.start can prefix their message with "schedule:" or "/schedule "
		// to route to schedule.request logic instead of starting a new general agent session.
		if scheduleText, ok := extractSchedulePrefix(p.Text); ok {
			return h.handleScheduleRequest(ctx, client, p.UserID, p.WorkDir, p.Source, scheduleText)
		}

		sess, err := h.sessions.StartSession(ctx, session.StartRequest{
			UserID:       p.UserID,
			Text:         p.Text,
			WorkDir:      p.WorkDir,
			Source:       p.Source,
			ThreadID:     p.ThreadID,
			ChannelID:    p.ChannelID,
			Broadcaster:  h.server,
			HITLNotifier: h.server,
		})
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}

		client.Subscribe(sess.ID)
		return map[string]any{"sessionId": sess.ID, "status": "started"}, nil

	case "task.steer":
		var p struct {
			SessionID string `json:"sessionId"`
			Text      string `json:"text"`
		}
		json.Unmarshal(req.Params, &p)
		err := h.sessions.Steer(p.SessionID, p.Text)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		return map[string]any{"ok": true}, nil

	case "task.abort":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		json.Unmarshal(req.Params, &p)
		err := h.sessions.Abort(p.SessionID)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		client.Unsubscribe(p.SessionID)
		return map[string]any{"ok": true}, nil

	case "hitl.respond":
		var p struct {
			SessionID string `json:"sessionId"`
			RequestID string `json:"requestId"`
			Decision  string `json:"decision"`
		}
		json.Unmarshal(req.Params, &p)
		err := h.sessions.ResolveHITL(p.SessionID, p.RequestID, p.Decision)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		return map[string]any{"ok": true}, nil

	case "session.list":
		var p struct {
			UserID string `json:"userId"`
			Status string `json:"status"`
		}
		json.Unmarshal(req.Params, &p)
		list := h.sessions.List(p.UserID, p.Status)
		return list, nil

	case "memory.get":
		var p struct {
			UserID string `json:"userId"`
		}
		json.Unmarshal(req.Params, &p)
		memCtx, err := h.memory.GetContextForUser(p.UserID)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		return map[string]any{"context": memCtx}, nil

	case "memory.update":
		var p struct {
			UserID string `json:"userId"`
			Key    string `json:"key"`
			Value  string `json:"value"`
		}
		json.Unmarshal(req.Params, &p)
		err := h.memory.UpdateUserFact(p.UserID, p.Key, p.Value)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		return map[string]any{"ok": true}, nil

	case "feedback.submit":
		// Submit explicit user feedback about incorrect/unexpected agent behavior.
		// Params: {sessionId?, userId, text, context?, expectedBehavior?}
		// Persists feedback, enriches the matching TaskOutcome, and triggers
		// MemEvolve if rate limits allow.
		var p struct {
			SessionID        string `json:"sessionId"`
			UserID           string `json:"userId"`
			Text             string `json:"text"`
			Context          string `json:"context"`
			ExpectedBehavior string `json:"expectedBehavior"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &RPCError{Code: -32602, Message: "invalid params: " + err.Error()}
		}
		fb := metrics.UserFeedback{
			SessionID:        p.SessionID,
			UserID:           p.UserID,
			FeedbackText:     p.Text,
			Context:          p.Context,
			ExpectedBehavior: p.ExpectedBehavior,
			Timestamp:        time.Now(),
		}
		if err := h.sessions.SubmitFeedback(fb); err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		return map[string]any{"ok": true}, nil

	case "schedule.list":
		var p struct {
			UserID string `json:"userId"`
		}
		json.Unmarshal(req.Params, &p)
		if p.UserID == "" {
			return nil, &RPCError{Code: -32602, Message: "userId required"}
		}
		if h.taskStore == nil {
			return nil, &RPCError{Code: -32000, Message: "schedule service not available"}
		}
		tasks, err := h.taskStore.List(p.UserID)
		if err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		return map[string]any{"tasks": tasks}, nil

	case "schedule.delete":
		var p struct {
			UserID string `json:"userId"`
			TaskID string `json:"taskId"`
		}
		json.Unmarshal(req.Params, &p)
		if p.UserID == "" || p.TaskID == "" {
			return nil, &RPCError{Code: -32602, Message: "userId and taskId required"}
		}
		if h.taskStore == nil {
			return nil, &RPCError{Code: -32000, Message: "schedule service not available"}
		}
		if err := h.taskStore.Delete(p.TaskID); err != nil {
			return nil, &RPCError{Code: -32000, Message: err.Error()}
		}
		return map[string]any{"ok": true}, nil

	case "health.check":
		return map[string]any{
			"status":         "ok",
			"activeSessions": h.sessions.ActiveCount(),
		}, nil

	default:
		return nil, &RPCError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

// extractFeedbackPrefix checks whether s begins with a feedback prefix
// ("feedback:" or "/feedback ") in a case-insensitive manner.
// If matched, it returns the stripped, trimmed feedback text and true.
// Supported prefixes:
//   - "feedback: some text"   → "some text"
//   - "feedback:some text"    → "some text"
//   - "/feedback some text"   → "some text"
func extractFeedbackPrefix(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)

	const colonPrefix = "feedback:"
	const slashPrefix = "/feedback "

	switch {
	case strings.HasPrefix(lower, colonPrefix):
		return strings.TrimSpace(trimmed[len(colonPrefix):]), true
	case strings.HasPrefix(lower, slashPrefix):
		return strings.TrimSpace(trimmed[len(slashPrefix):]), true
	default:
		return "", false
	}
}

// extractSchedulePrefix checks whether s begins with a schedule prefix
// ("schedule:" or "/schedule ") in a case-insensitive manner.
// If matched, it returns the stripped, trimmed schedule request text and true.
// Supported prefixes:
//   - "schedule: some text"   → "some text"
//   - "schedule:some text"    → "some text"
//   - "/schedule some text"   → "some text"
func extractSchedulePrefix(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)

	const colonPrefix = "schedule:"
	const slashPrefix = "/schedule "

	switch {
	case strings.HasPrefix(lower, colonPrefix):
		return strings.TrimSpace(trimmed[len(colonPrefix):]), true
	case strings.HasPrefix(lower, slashPrefix):
		return strings.TrimSpace(trimmed[len(slashPrefix):]), true
	default:
		return "", false
	}
}

// handleScheduleRequest routes a schedule request to an agent session with the
// schedule-task skill pre-loaded. The agent will interact with the user to gather
// scheduling details and will call the Schedule_task tool to persist the task.
func (h *Handler) handleScheduleRequest(ctx context.Context, client *Client, userID string, workDir string, source string, scheduleText string) (any, *RPCError) {
	if h.skills == nil {
		return nil, &RPCError{Code: -32000, Message: "skills registry not available"}
	}

	// Load the schedule-task skill
	skill, err := h.skills.Get("schedule-task")
	if err != nil {
		slog.Warn("schedule-task skill not found", "err", err)
		return nil, &RPCError{Code: -32000, Message: "schedule-task skill not found"}
	}

	// Inject skill instructions into the task text
	skillPrefix := fmt.Sprintf("You must use the schedule-task skill to guide the user.\n\nSchedule-Task Skill Instructions:\n%s\n\n---\n\n", skill.Instructions)
	taskWithSkill := skillPrefix + scheduleText

	// Start a normal agent session with the skill-injected task
	sess, err := h.sessions.StartSession(ctx, session.StartRequest{
		UserID:       userID,
		Text:         taskWithSkill,
		WorkDir:      workDir,
		Source:       source,
		Broadcaster:  h.server,
		HITLNotifier: h.server,
	})
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}

	client.Subscribe(sess.ID)
	return map[string]any{"sessionId": sess.ID, "status": "started", "routed": "schedule"}, nil
}
