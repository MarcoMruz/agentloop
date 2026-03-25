package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MarcoMruz/agentloop/internal/bridge"
	"github.com/MarcoMruz/agentloop/internal/config"
)

// LLMClient provides background LLM operations for memory management.
// This is the "cheap model tier" — a separate PiConfig entry (e.g. claude-haiku)
// distinct from the main agent model.
type LLMClient interface {
	// Embed generates a dense vector embedding for the given text.
	// Returns nil (no error) when embeddings are unavailable.
	Embed(text string) ([]float32, error)
	// Complete runs a short completion for structured extraction tasks.
	// Returns empty string on noop.
	Complete(prompt string) (string, error)
}

// PiCompletionClient spawns a short-lived pi subprocess per Complete() call.
type PiCompletionClient struct {
	piCfg config.PiConfig
}

// NewPiCompletionClient creates a client backed by a pi subprocess.
func NewPiCompletionClient(piCfg config.PiConfig) *PiCompletionClient {
	return &PiCompletionClient{piCfg: piCfg}
}

// Complete spawns a pi subprocess, sends the prompt, collects the text response, and exits.
func (c *PiCompletionClient) Complete(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	b := bridge.New(c.piCfg, config.SecurityConfig{}, config.HITLConfig{})

	var mu sync.Mutex
	var sb strings.Builder
	doneCh := make(chan struct{})

	b.SetEventHandler(func(event bridge.RPCEvent) error {
		if event.Type == "message_update" &&
			event.AssistantMessageEvent != nil &&
			event.AssistantMessageEvent.Type == "text_delta" {
			mu.Lock()
			sb.WriteString(event.AssistantMessageEvent.Delta)
			mu.Unlock()
		}
		if event.Type == "agent_end" {
			select {
			case <-doneCh:
			default:
				close(doneCh)
			}
		}
		return nil
	})

	if err := b.Start(ctx, ""); err != nil {
		return "", fmt.Errorf("pi completion start: %w", err)
	}
	defer b.Stop() //nolint:errcheck

	if err := b.Prompt(ctx, "completion", prompt); err != nil {
		return "", fmt.Errorf("pi completion prompt: %w", err)
	}

	select {
	case <-doneCh:
	case <-b.Done():
	case <-ctx.Done():
		return "", fmt.Errorf("pi completion timeout: %w", ctx.Err())
	}

	mu.Lock()
	result := sb.String()
	mu.Unlock()
	return result, nil
}

// Embed always returns nil — pi subprocesses do not expose embedding APIs.
func (c *PiCompletionClient) Embed(_ string) ([]float32, error) { return nil, nil }

// NoopClient is used when memory.agent.enabled = false.
type NoopClient struct{}

func (n *NoopClient) Embed(_ string) ([]float32, error) { return nil, nil }
func (n *NoopClient) Complete(_ string) (string, error)  { return "", nil }
