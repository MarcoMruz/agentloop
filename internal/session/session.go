package session

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/MarcoMruz/agentloop/internal/agent"
	"github.com/MarcoMruz/agentloop/internal/vault"
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
	ID           string
	UserID       string
	Task         string
	WorkDir      string
	Source       string
	State        State
	StartedAt    time.Time
	LastActivity time.Time

	// Thread/context isolation fields.
	ThreadID              string
	ChannelID             string
	ConversationContextID string // e.g. "C123456:1234567890.000100" for Slack threads

	hitlDenials int32
	steerCount  int32

	mu          sync.Mutex
	steerCh     chan string
	abortCh     chan struct{}
	hitlPending map[string]chan string // requestId → decision channel
	result      *agent.RunResult
}

func NewSession(userId, task, workDir, source, channelID, threadID string) *Session {
	now := time.Now()
	return &Session{
		ID:           fmt.Sprintf("sess-%s", uuid.New().String()[:8]),
		UserID:       userId,
		Task:         task,
		WorkDir:      workDir,
		Source:       source,
		State:        StateStarting,
		StartedAt:    now,
		LastActivity: now,
		ChannelID:    channelID,
		ThreadID:     threadID,
		ConversationContextID: func() string {
			if channelID == "" || threadID == "" {
				return ""
			}
			return channelID + ":" + threadID
		}(),
		steerCh:     make(chan string, 5),
		abortCh:     make(chan struct{}),
		hitlPending: make(map[string]chan string),
	}
}

// GetConversationContextID returns the context ID for memory scoping.
func (s *Session) GetConversationContextID() string {
	return s.ConversationContextID
}

// Touch updates LastActivity to now. Called on every text response from the agent.
func (s *Session) Touch() {
	s.mu.Lock()
	s.LastActivity = time.Now()
	s.mu.Unlock()
}

func (s *Session) GetLastActivity() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastActivity
}

func (s *Session) Info() SessionInfo {
	return SessionInfo{
		ID: s.ID, UserID: s.UserID, Task: s.Task,
		State: s.State, Source: s.Source, Start: s.StartedAt.Format(time.RFC3339),
	}
}

// SetRunning transitions the session from StateStarting to StateRunning.
// Called by the session manager once the agent goroutine is active.
func (s *Session) SetRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == StateStarting {
		s.State = StateRunning
	}
}

func (s *Session) Steer(text string) error {
	atomic.AddInt32(&s.steerCount, 1)
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
	// Do NOT delete from hitlPending here — WaitHITL must be able to look it up
	// by requestId after this call returns (especially in the auto-approve path where
	// ResolveHITL is called synchronously before WaitHITL). WaitHITL will clean up.
	s.State = StateRunning
	s.mu.Unlock()

	if decision == "deny" {
		atomic.AddInt32(&s.hitlDenials, 1)
	}
	ch <- decision // buffered (size 1), never blocks
	return nil
}

// HITLDenialCount returns the number of HITL denials in this session.
func (s *Session) HITLDenialCount() int {
	return int(atomic.LoadInt32(&s.hitlDenials))
}

// SteerCount returns the number of steer commands in this session.
func (s *Session) SteerCount() int {
	return int(atomic.LoadInt32(&s.steerCount))
}

func (s *Session) WaitHITL(requestId string, timeout time.Duration) (string, error) {
	s.mu.Lock()
	ch, ok := s.hitlPending[requestId]
	s.mu.Unlock()
	if !ok { return "", fmt.Errorf("no pending HITL %q", requestId) }

	select {
	case decision := <-ch:
		// Clean up map entry now that we've consumed the decision.
		s.mu.Lock()
		delete(s.hitlPending, requestId)
		s.mu.Unlock()
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
			ThreadID:              s.ThreadID,
			ChannelID:             s.ChannelID,
			ConversationContextID: s.ConversationContextID,
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
