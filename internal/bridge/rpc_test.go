package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBuildSafeEnv(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "secret")
	os.Setenv("SAFE_VAR", "ok")
	defer os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Unsetenv("SAFE_VAR")

	env := buildSafeEnv([]string{"ANTHROPIC_", "OPENAI_"})
	for _, e := range env {
		if strings.HasPrefix(e, "ANTHROPIC_") {
			t.Fatal("SECURITY: sensitive env var leaked")
		}
	}
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "SAFE_VAR=") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected SAFE_VAR to be preserved")
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
