package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/user/agentloop/internal/agent"
	"github.com/user/agentloop/internal/vault"
)

type State string

const (
	StateStarting    State = "starting"
	StateRunning     State = "running"
	StateWaitingHITL State = "waiting_hitl"
	StateDone        State = "done"
	StateAborted     State = "aborted"
	StateError       State = "error"
)

type SessionInfo struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Task   string `json:"task"`
	State  State  `json:"state"`
	Source string `json:"source"`
	Start  string `json:"startedAt"`
}

type Session struct {
	ID        string
	UserID    string
	Task      string
	WorkDir   string
	Source    string
	State     State
	StartedAt time.Time

	mu           sync.Mutex
	steerCh      chan string
	abortCh      chan struct{}
	hitlPending  map[string]chan string // requestId → decision channel
	result       *agent.RunResult
}

func NewSession(userId, task, workDir, source string) *Session {
	return &Session{
		ID:          fmt.Sprintf("sess-%s", uuid.New().String()[:8]),
		UserID:      userId,
		Task:        task,
		WorkDir:     workDir,
		Source:      source,
		State:       StateStarting,
		StartedAt:   time.Now(),
		steerCh:     make(chan string, 5),
		abortCh:     make(chan struct{}),
		hitlPending: make(map[string]chan string),
	}
}

func (s *Session) Info() SessionInfo {
	return SessionInfo{
		ID: s.ID, UserID: s.UserID, Task: s.Task,
		State: s.State, Source: s.Source, Start: s.StartedAt.Format(time.RFC3339),
	}
}

func (s *Session) Steer(text string) error {
	select {
	case s.steerCh <- text:
		return nil
	default:
		return fmt.Errorf("steer channel full")
	}
}

func (s *Session) SteerCh() <-chan string { return s.steerCh }

func (s *Session) Abort() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == StateDone || s.State == StateAborted { return }
	s.State = StateAborted
	select {
	case <-s.abortCh: // already closed
	default:
		close(s.abortCh)
	}
}

func (s *Session) AbortCh() <-chan struct{} { return s.abortCh }

func (s *Session) SetPendingHITL(requestId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = StateWaitingHITL
	s.hitlPending[requestId] = make(chan string, 1)
}

func (s *Session) ResolveHITL(requestId string, decision string) error {
	s.mu.Lock()
	ch, ok := s.hitlPending[requestId]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("no pending HITL request %q", requestId)
	}
	delete(s.hitlPending, requestId)
	s.State = StateRunning
	s.mu.Unlock()

	ch <- decision
	return nil
}

func (s *Session) WaitHITL(requestId string, timeout time.Duration) (string, error) {
	s.mu.Lock()
	ch, ok := s.hitlPending[requestId]
	s.mu.Unlock()
	if !ok { return "", fmt.Errorf("no pending HITL %q", requestId) }

	select {
	case decision := <-ch:
		return decision, nil
	case <-time.After(timeout):
		s.mu.Lock()
		delete(s.hitlPending, requestId)
		s.State = StateRunning
		s.mu.Unlock()
		return "deny", nil // timeout → auto-deny
	case <-s.abortCh:
		return "abort", nil
	}
}

func (s *Session) SetResult(r agent.RunResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = &r
	if r.Error != "" {
		s.State = StateError
	} else {
		s.State = StateDone
	}
}

func (s *Session) ToVaultNote(r agent.RunResult) vault.SessionNote {
	return vault.SessionNote{
		Frontmatter: vault.SessionFrontmatter{
			ID: s.ID, Title: truncateStr(s.Task, 60),
			Created: s.StartedAt, Updated: time.Now(),
			Status: string(s.State), Provider: "", Model: "",
			Source: s.Source, UserID: s.UserID,
		},
		TaskText:   s.Task,
		Transcript: r.Output,
		ToolCalls:  r.ToolsUsed,
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n] + "..."
}
