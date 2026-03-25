package pirun

import (
	"context"
	"strings"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/config"
)

func TestRunTextSessionSignature(t *testing.T) {
	// Verify the function exists and has the right signature.
	// We cannot call it without a real pi binary, so we just verify compilation
	// and that calling with a cancelled context returns an error quickly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	piCfg := config.PiConfig{Binary: "pi", Provider: "anthropic", Model: "claude-haiku-4-5-20251001"}
	secCfg := config.SecurityConfig{}

	_, err := RunTextSession(ctx, piCfg, secCfg, t.TempDir(), "test", "hello")
	if err == nil {
		t.Skip("pi binary available — skipping compile-only test")
	}
	if !strings.Contains(err.Error(), "start pi") && !strings.Contains(err.Error(), "context") {
		t.Errorf("unexpected error: %v", err)
	}
}
