package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/logging"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/baseline"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/meta"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
	"github.com/MarcoMruz/agentloop/internal/memory/llm"
	"github.com/MarcoMruz/agentloop/internal/memory/notes"
	"github.com/MarcoMruz/agentloop/internal/server"
	"github.com/MarcoMruz/agentloop/internal/session"
	"github.com/MarcoMruz/agentloop/internal/skills"
	"github.com/MarcoMruz/agentloop/internal/vault"
)

var version = "dev"

func main() {
	// Allow overriding config path via --config flag or AGENTLOOP_CONFIG env var
	cfgPath := config.DefaultConfigPath()
	if v := os.Getenv("AGENTLOOP_CONFIG"); v != "" {
		cfgPath = v
	}
	for i, arg := range os.Args[1:] {
		if arg == "--config" && i+1 < len(os.Args[1:]) {
			cfgPath = os.Args[i+2]
			break
		}
		if len(arg) > 9 && arg[:9] == "--config=" {
			cfgPath = arg[9:]
			break
		}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logging.Init(cfg.Logging.Level, cfg.Logging.File)

	// Initialize vault
	v, err := vault.New(cfg.Vault.Path)
	if err != nil {
		slog.Error("vault init failed", "error", err)
		os.Exit(1)
	}

	// Initialize memory engine
	mem := memory.NewEngine(
		cfg.Vault.Path,
		cfg.Memory.MaxContextTokens,
		cfg.Memory.CompactionStrategy,
		cfg.Memory.ConversationRetainDays,
	)

	// Background LLM client (pi subprocess tier for memory ops)
	var llmClient llm.LLMClient = &llm.NoopClient{}
	if cfg.Memory.Agent.Enabled {
		agentPiCfg := config.PiConfig{
			Binary:   cfg.Memory.Agent.Binary,
			Provider: cfg.Memory.Agent.Provider,
			Model:    cfg.Memory.Agent.Model,
		}
		llmClient = llm.NewPiCompletionClient(agentPiCfg)
	}
	mem.SetLLMClient(llmClient)

	// Atomic notes store (SQLite + sqlite-vec)
	var sqliteStore *notes.SQLiteNoteStore
	if s, err := notes.NewSQLiteNoteStore(v.NotesDir(), cfg.Memory.EmbeddingDims); err == nil {
		sqliteStore = s
		mem.SetNoteStore(sqliteStore)
	} else {
		slog.Warn("atomic notes store unavailable, running without it", "err", err)
	}

	// Install default agent instructions to vault (if not already present)
	v.InstallDefaultAgents("configs/agents")

	// Initialize skills registry
	sk := skills.NewRegistry(cfg.Skills.SkillDirs)

	// Initialize session manager
	sm := session.NewManager(cfg, v, mem, sk)

	// Construct skill agent (inherits pi config if agent-specific fields are blank)
	skillPiCfg := cfg.Pi
	if cfg.Skills.Agent.Binary != "" {
		skillPiCfg.Binary = cfg.Skills.Agent.Binary
	}
	if cfg.Skills.Agent.Provider != "" {
		skillPiCfg.Provider = cfg.Skills.Agent.Provider
	}
	if cfg.Skills.Agent.Model != "" {
		skillPiCfg.Model = cfg.Skills.Agent.Model
	}
	skillAgent := skills.NewSkillAgent(skillPiCfg, cfg.Security)
	sm.SetSkillAgent(skillAgent)

	// MemEvolve: pipeline, collector, and meta-agent
	if cfg.Evolution.Enabled {
		if err := evolve.EnsureEvolvedDirs(cfg.Vault.Path); err != nil {
			slog.Warn("failed to create evolved dirs", "err", err)
		} else {
			profiles := mem.Profiles()
			compactor := memory.NewCompactor(cfg.Memory.CompactionStrategy)

			enc := baseline.NewBaselineEncoder(profiles)
			stor := baseline.NewBaselineStorer(profiles, mem)
			mgr := baseline.NewBaselineManager(compactor)

			defaults := evolve.DefaultPipelineConfig()
			var retr *baseline.BaselineRetriever
			if sqliteStore != nil {
				retr = baseline.NewBaselineRetriever(profiles, sqliteStore, &defaults.Retriever)
			}

			pipeline := evolve.NewPipelineHolder(cfg.Vault.Path, defaults, enc, stor, retr, mgr)
			mem.SetPipeline(pipeline)

			evolvedMetricsDir := filepath.Join(cfg.Vault.Path, "memory", "evolved")
			collector := metrics.NewCollector(
				evolvedMetricsDir,
				cfg.Evolution.ScoreThreshold,
				cfg.Evolution.MinCooldownSeconds,
				cfg.Evolution.MaxDailyRuns,
			)

			agentsMDPath, _ := filepath.Abs("agents-md/AGENTS.md")
			skillsPath := filepath.Join(cfg.Vault.Path, "skills")
			metaAgent := meta.NewMetaAgent(
				cfg.Vault.Path,
				agentsMDPath,
				skillsPath,
				cfg.Pi,
				cfg.Security,
				cfg.Evolution,
				pipeline,
				mem,
			)

			sm.SetMetricsCollector(collector)
			sm.SetPipeline(pipeline)
			sm.SetMetaAgent(metaAgent)

			slog.Info("MemEvolve enabled",
				"score_threshold", cfg.Evolution.ScoreThreshold,
				"cooldown_s", cfg.Evolution.MinCooldownSeconds,
				"daily_cap", cfg.Evolution.MaxDailyRuns,
			)
		}
	}

	// Initialize server
	handler := server.NewHandler(sm, mem)
	srv := server.New(expandHome(cfg.Server.SocketPath), handler)
	handler.SetServer(srv)

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		slog.Info("shutting down...")
		sm.StopAll()
		srv.Stop()
	}()

	slog.Info("AgentLoop server starting", "version", version, "socket", cfg.Server.SocketPath)
	if err := srv.Start(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func expandHome(p string) string {
	if len(p) > 1 && p[:2] == "~/" {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}
