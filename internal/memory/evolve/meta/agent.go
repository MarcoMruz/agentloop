package meta

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
	"github.com/MarcoMruz/agentloop/internal/pirun"
)

// UserFeedback captures explicit feedback from a user about incorrect or unexpected agent behavior.
type UserFeedback struct {
	// Text is the human-readable description of what went wrong.
	Text string
	// UserID identifies whose memory/skills should be updated.
	UserID string
	// SessionID optionally links the feedback to a specific past session.
	// When non-empty, Evolve will try to load the real outcome for that session.
	SessionID string
}

// OnCompleteFunc is called after a successful evolution with the userId, summary,
// and the full proposal. Use it to push event.evolution_complete to clients.
type OnCompleteFunc func(userId, summary string, proposal EvolutionProposal)

type MetaAgent struct {
	mu           sync.Mutex
	vaultPath    string
	agentsMDPath string
	skillsPath   string
	piCfg        config.PiConfig
	secCfg       config.SecurityConfig
	evoCfg       config.EvolutionConfig
	pipeline     *evolve.PipelineHolder
	engine       *memory.Engine // nil = note proposals skipped
	// OnComplete is called after each successful evolution. Optional.
	OnComplete OnCompleteFunc
}

func NewMetaAgent(
	vaultPath, agentsMDPath, skillsPath string,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
	evoCfg config.EvolutionConfig,
	pipeline *evolve.PipelineHolder,
	engine *memory.Engine,
) *MetaAgent {
	return &MetaAgent{
		vaultPath:    vaultPath,
		agentsMDPath: agentsMDPath,
		skillsPath:   skillsPath,
		piCfg:        piCfg,
		secCfg:       secCfg,
		evoCfg:       evoCfg,
		pipeline:     pipeline,
		engine:       engine,
	}
}

// Evolve analyses the given outcome (and its cluster) and applies an evolution proposal.
// An optional UserFeedback can be passed to incorporate explicit user-reported issues.
func (m *MetaAgent) Evolve(ctx context.Context, outcome metrics.TaskOutcome, feedback ...UserFeedback) {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("evolution starting", "session", outcome.SessionID, "score", outcome.Score())

	allOutcomes, err := metrics.LoadOutcomes(
		m.vaultPath,
		outcome.UserID,
		30,
	)
	if err != nil {
		slog.Error("failed to load outcomes", "error", err)
		return
	}

	clusters := metrics.ClusterOutcomes(allOutcomes, m.evoCfg.MaxOutcomesPerRun)
	cluster := metrics.FindClusterFor(clusters, outcome.SessionID)
	if cluster == nil {
		cluster = []metrics.TaskOutcome{outcome}
	}

	currentConfig := evolve.DefaultPipelineConfig()
	if m.pipeline != nil {
		if p := m.pipeline.Get(); p != nil {
			currentConfig = p.Config()
		}
	}

	agentsMDEvolved := m.readEvolvedSection()

	var feedbackText string
	if len(feedback) > 0 && feedback[0].Text != "" {
		feedbackText = feedback[0].Text
	}

	prompt := BuildEvolutionPrompt(EvolutionPrompt{
		SystemContext: DefaultSystemContext(),
		Outcomes:      cluster,
		CurrentConfig: currentConfig,
		AgentsMD:      agentsMDEvolved,
		UserFeedback:  feedbackText,
		Constraints: []string{
			"All skill names must be prefixed with 'evolved-'",
			"AGENTS.md changes are limited to the EVOLVED section",
			"Config changes should be conservative — small tweaks, not rewrites",
			"Maintain vault compatibility (Markdown/YAML format)",
			"Position must remain 'edges' for retriever (Lost in the Middle)",
		},
	})

	workDir, err := os.MkdirTemp("", "memevolve-*")
	if err != nil {
		slog.Error("failed to create temp dir", "error", err)
		return
	}
	defer os.RemoveAll(workDir)
	response, err := pirun.RunTextSession(ctx, m.piCfg, m.secCfg, workDir, "meta-evolve", prompt)
	if err != nil {
		slog.Error("pi session failed", "error", err)
		return
	}

	proposal, err := ParseProposal(response)
	if err != nil {
		slog.Error("failed to parse evolution proposal", "error", err)
		return
	}

	applier := NewApplier(m.vaultPath, m.agentsMDPath, m.skillsPath, m.engine)
	if err := applier.Apply(proposal, outcome.UserID); err != nil {
		slog.Error("failed to apply evolution", "error", err)
		return
	}

	if m.pipeline != nil {
		if err := m.pipeline.Reload(); err != nil {
			slog.Error("pipeline reload failed", "error", err)
		}
	}

	slog.Info("evolution complete", "summary", proposal.Summary)

	if m.OnComplete != nil {
		m.OnComplete(outcome.UserID, proposal.Summary, *proposal)
	}
}


// EvolveFromFeedback triggers an evolution driven by explicit user feedback rather than
// a metrics threshold breach. It attempts to load the real TaskOutcome for the referenced
// session; if none is found it constructs a synthetic one so the meta-agent has context.
func (m *MetaAgent) EvolveFromFeedback(ctx context.Context, fb UserFeedback) {
	if fb.UserID == "" {
		slog.Warn("EvolveFromFeedback: UserID is empty, skipping")
		return
	}

	// Try to find the real outcome first.
	var outcome metrics.TaskOutcome
	found := false

	if fb.SessionID != "" {
		all, err := metrics.LoadOutcomes(m.vaultPath, fb.UserID, 90)
		if err == nil {
			for _, o := range all {
				if o.SessionID == fb.SessionID {
					outcome = o
					found = true
					break
				}
			}
		}
	}

	if !found {
		// Build a synthetic outcome that will anchor the cluster lookup.
		outcome = metrics.TaskOutcome{
			SessionID:   fmt.Sprintf("feedback-%d", time.Now().UnixMilli()),
			UserID:      fb.UserID,
			FinalStatus: "feedback",
			Timestamp:   time.Now(),
			Feedback:    fb.Text,
		}
	}

	m.Evolve(ctx, outcome, fb)
}

func (m *MetaAgent) readEvolvedSection() string {
	if m.agentsMDPath == "" {
		return ""
	}
	data, err := os.ReadFile(m.agentsMDPath)
	if err != nil {
		return ""
	}
	content := string(data)
	start := strings.Index(content, "<!-- EVOLVED:START -->")
	end := strings.Index(content, "<!-- EVOLVED:END -->")
	if start == -1 || end == -1 {
		return ""
	}
	return content[start+len("<!-- EVOLVED:START -->") : end]
}
