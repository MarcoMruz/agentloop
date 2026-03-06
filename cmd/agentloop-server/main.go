package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/logging"
	"github.com/MarcoMruz/agentloop/internal/memory"
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

	// Initialize skills registry
	sk := skills.NewRegistry(cfg.Skills.SkillDirs)

	// Initialize session manager
	sm := session.NewManager(cfg, v, mem, sk)

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
