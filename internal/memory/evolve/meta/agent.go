package meta

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
	"github.com/MarcoMruz/agentloop/internal/pirun"
)

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

func (m *MetaAgent) Evolve(ctx context.Context, outcome metrics.TaskOutcome) {
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

	prompt := BuildEvolutionPrompt(EvolutionPrompt{
		SystemContext: DefaultSystemContext(),
		Outcomes:      cluster,
		CurrentConfig: currentConfig,
		AgentsMD:      agentsMDEvolved,
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
