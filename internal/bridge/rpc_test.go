package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/config"
)

func TestBuildSafeEnv(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "secret")
	os.Setenv("SAFE_VAR", "ok")
	defer os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Unsetenv("SAFE_VAR")

	injectionCfg := config.InjectionConfig{
		EnableProtection: true,
		WhitelistSources: []string{"~/projects", "~/sandbox"},
		BlockedKeywords:  []string{"test-keyword"},
		MaxContentLength: 10000,
		ApprovalTier:     "owner",
		SanitizeMemory:   true,
	}

	hitlCfg := config.HITLConfig{
		ForceHITLKeywords: []string{"sudo", "chmod", "systemctl"},
	}

	env := buildSafeEnv([]string{"ANTHROPIC_", "OPENAI_"}, injectionCfg, hitlCfg)

	// Check that sensitive env vars are stripped
	for _, e := range env {
		if strings.HasPrefix(e, "ANTHROPIC_") {
			t.Fatal("SECURITY: sensitive env var leaked")
		}
	}

	// Check that safe vars are preserved
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "SAFE_VAR=") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected SAFE_VAR to be preserved")
	}

	// Check injection protection env vars are set
	expectedVars := map[string]bool{
		"AGENTLOOP_INJECTION_PROTECTION=true":              false,
		"AGENTLOOP_WHITELIST_SOURCES=~/projects,~/sandbox": false,
		"AGENTLOOP_BLOCKED_KEYWORDS=test-keyword":          false,
		"AGENTLOOP_MAX_CONTENT_LENGTH=10000":               false,
		"AGENTLOOP_APPROVAL_TIER=owner":                    false,
		"AGENTLOOP_SANITIZE_MEMORY=true":                   false,
		"AGENTLOOP_HITL_FORCE_KEYWORDS=sudo,chmod,systemctl": false,
	}

	for _, envVar := range env {
		for expected := range expectedVars {
			if envVar == expected {
				expectedVars[expected] = true
			}
		}
	}

	for expected, found := range expectedVars {
		if !found {
			t.Fatalf("SECURITY: missing injection protection env var: %s", expected)
		}
	}
}

func TestRPCCommandSerialization(t *testing.T) {
	cmd := RPCCommand{Type: "prompt", ID: "test-1", Message: "hello world"}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "prompt" || parsed["id"] != "test-1" || parsed["message"] != "hello world" {
		t.Fatalf("unexpected serialization: %s", string(data))
	}
	// Ensure old "text" field is not present
	if _, hasText := parsed["text"]; hasText {
		t.Fatalf("PROTOCOL: 'text' field must not be used, pi expects 'message': %s", string(data))
	}
}

func TestExtensionUIResponseSerialization(t *testing.T) {
	confirmed := true
	resp := ExtensionUIResponse{Type: "extension_ui_response", ID: "req-1", Confirmed: &confirmed}
	data, _ := json.Marshal(resp)
	if !strings.Contains(string(data), `"confirmed":true`) {
		t.Fatalf("unexpected: %s", string(data))
	}
}

func TestMemoryToolEventFromToolArgs(t *testing.T) {
	event := MemoryToolEvent{
		Operation: "add",
		Content:   "user prefers dark mode",
		Keywords:  []string{"ui", "dark-mode"},
		Tags:      []string{"preference"},
	}
	if event.Operation != "add" {
		t.Fatalf("operation: %q", event.Operation)
	}
	if event.Content != "user prefers dark mode" {
		t.Fatalf("content: %q", event.Content)
	}
	if len(event.Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got %d", len(event.Keywords))
	}
}

func TestIsMemoryTool(t *testing.T) {
	cases := []struct {
		name     string
		expected bool
	}{
		{"Add_memory", true},
		{"Update_memory", true},
		{"Delete_memory", true},
		{"bash", false},
		{"read", false},
		{"add_memory", false}, // case-sensitive
	}
	for _, tc := range cases {
		if got := isMemoryTool(tc.name); got != tc.expected {
			t.Errorf("isMemoryTool(%q) = %v, want %v", tc.name, got, tc.expected)
		}
	}
}
