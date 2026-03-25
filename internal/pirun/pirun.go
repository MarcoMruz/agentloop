package pirun

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	b.SetEventHandler(func(event bridge.RPCEvent) error {
		if event.Type == "message_update" && event.AssistantMessageEvent != nil {
			if event.AssistantMessageEvent.Type == "text_delta" {
				response.WriteString(event.AssistantMessageEvent.Delta)
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

	<-b.Done()
	return response.String(), nil
}
