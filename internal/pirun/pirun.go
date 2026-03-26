package pirun

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/MarcoMruz/agentloop/internal/bridge"
	"github.com/MarcoMruz/agentloop/internal/config"
)

// RunTextSession starts a pi subprocess, sends a single prompt, collects the
// full text response, and returns it. The subprocess exits after responding.
// Pass os.TempDir() as workDir when no real working directory is needed.
func RunTextSession(
	ctx context.Context,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
	workDir string,
	promptID string,
	prompt string,
) (string, error) {
	if workDir == "" {
		workDir = os.TempDir()
	}

	b := bridge.New(piCfg, secCfg, config.HITLConfig{})

	var response strings.Builder

	// doneCh closes on agent_end; pi stays alive in --mode rpc so we cannot
	// rely on b.Done() (process exit) to signal completion.
	doneCh := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(doneCh) }) }

	b.SetEventHandler(func(event bridge.RPCEvent) error {
		switch event.Type {
		case "message_update":
			if event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == "text_delta" {
				response.WriteString(event.AssistantMessageEvent.Delta)
			}
		case "agent_end":
			closeDone()
		case "auto_retry_end":
			if !event.Success {
				closeDone()
			}
		}
		return nil
	})

	if err := b.Start(ctx, workDir); err != nil {
		return "", fmt.Errorf("start pi: %w", err)
	}
	defer b.Stop()

	if err := b.Prompt(ctx, promptID, prompt); err != nil {
		return "", fmt.Errorf("prompt pi: %w", err)
	}

	select {
	case <-doneCh:
	case <-b.Done(): // pi process exited unexpectedly
	case <-ctx.Done():
		return response.String(), ctx.Err()
	}
	return response.String(), nil
}
