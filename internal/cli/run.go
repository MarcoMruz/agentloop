package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/user/agentloop/internal/orchestrator"
	"github.com/user/agentloop/internal/vault"
)

var runCmd = &cobra.Command{
	Use: "run [task description]", Short: "Run a task to completion",
	Args: cobra.MinimumNArgs(1), RunE: runTask,
}

func runTask(cmd *cobra.Command, args []string) error {
	task := strings.Join(args, " ")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	v, err := vault.New(cfg.Vault.Path)
	if err != nil {
		return fmt.Errorf("vault init: %w", err)
	}
	wd, _ := os.Getwd()
	orch := orchestrator.New(cfg, v)
	return orch.RunTask(ctx, task, wd)
}
