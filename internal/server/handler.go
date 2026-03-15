package server

import (
	"context"
	"encoding/json"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/session"
)

type Handler struct {
	sessions *session.Manager
	memory   *memory.Engine
	server   *Server // back-reference for broadcasting
}

func NewHandler(sm *session.Manager, mem *memory.Engine) *Handler {
	return &Handler{sessions: sm, memory: mem}
}

func (h *Handler) SetServer(s *Server) { h.server = s }

func (h *Handler) Handle(ctx context.Context, client *Client, req JSONRPCRequest) (any, *RPCError) {
	switch req.Method {

	case "task.start":
		var p struct {
			UserID    string `json:"userId"`
			Text      string `json:"text"`
			WorkDir   string `json:"workDir"`
			Source    string `json:"source"`
			// Slack thread metadata (optional)
			ThreadID  string `json:"thread_id"`
			ChannelID string `json:"channel_id"`
		}
		json.Unmarshal(req.Params, &p)
		if p.UserID == "" || p.Text == "" {
			return nil, &RPCError{Code: -32602, Message: "userId and text required"}
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

		// Subscribe this client to session events
		client.Subscribe(sess.ID)

		return map[string]any{"sessionId": sess.ID, "status": "started"}, nil

	case "task.steer":
		var p struct {
			SessionID string `json:"sessionId"`
			Text      string `json:"text"`
		}
		json.Unmarshal(req.Params, &p)
		err := h.sessions.Steer(p.SessionID, p.Text)
		if err != nil { return nil, &RPCError{Code: -32000, Message: err.Error()} }
		return map[string]any{"ok": true}, nil

	case "task.abort":
		var p struct { SessionID string `json:"sessionId"` }
		json.Unmarshal(req.Params, &p)
		err := h.sessions.Abort(p.SessionID)
		if err != nil { return nil, &RPCError{Code: -32000, Message: err.Error()} }
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
		if err != nil { return nil, &RPCError{Code: -32000, Message: err.Error()} }
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
		var p struct { UserID string `json:"userId"` }
		json.Unmarshal(req.Params, &p)
		memCtx, err := h.memory.GetContextForUser(p.UserID)
		if err != nil { return nil, &RPCError{Code: -32000, Message: err.Error()} }
		return map[string]any{"context": memCtx}, nil

	case "memory.update":
		var p struct { UserID string `json:"userId"`; Key string `json:"key"`; Value string `json:"value"` }
		json.Unmarshal(req.Params, &p)
		err := h.memory.UpdateUserFact(p.UserID, p.Key, p.Value)
		if err != nil { return nil, &RPCError{Code: -32000, Message: err.Error()} }
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
