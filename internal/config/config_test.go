package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsReturnsNonNil(t *testing.T) {
	cfg := Defaults()
	if cfg == nil {
		t.Fatal("Defaults() returned nil")
	}
}

func TestDefaultsServerSocketPath(t *testing.T) {
	cfg := Defaults()
	if cfg.Server.SocketPath == "" {
		t.Fatal("Server.SocketPath should not be empty")
	}
}

func TestDefaultsPiBinary(t *testing.T) {
	cfg := Defaults()
	if cfg.Pi.Binary != "pi" {
		t.Fatalf("expected Pi.Binary %q, got %q", "pi", cfg.Pi.Binary)
	}
}

func TestDefaultsPiProvider(t *testing.T) {
	cfg := Defaults()
	if cfg.Pi.Provider != "anthropic" {
		t.Fatalf("expected Pi.Provider %q, got %q", "anthropic", cfg.Pi.Provider)
	}
}

func TestDefaultsPiModel(t *testing.T) {
	cfg := Defaults()
	if cfg.Pi.Model == "" {
		t.Fatal("Pi.Model should not be empty")
	}
}

func TestDefaultsVaultPath(t *testing.T) {
	cfg := Defaults()
	if cfg.Vault.Path == "" {
		t.Fatal("Vault.Path should not be empty")
	}
}

func TestDefaultsMemoryPositiveValues(t *testing.T) {
	cfg := Defaults()
	if cfg.Memory.MaxProfileEntries <= 0 {
		t.Fatalf("MaxProfileEntries should be positive, got %d", cfg.Memory.MaxProfileEntries)
	}
	if cfg.Memory.ConversationRetainDays <= 0 {
		t.Fatalf("ConversationRetainDays should be positive, got %d", cfg.Memory.ConversationRetainDays)
	}
	if cfg.Memory.CompactionThreshold <= 0 {
		t.Fatalf("CompactionThreshold should be positive, got %d", cfg.Memory.CompactionThreshold)
	}
	if cfg.Memory.MaxContextTokens <= 0 {
		t.Fatalf("MaxContextTokens should be positive, got %d", cfg.Memory.MaxContextTokens)
	}
	if cfg.Memory.PromptCacheTTLMinutes <= 0 {
		t.Fatalf("PromptCacheTTLMinutes should be positive, got %d", cfg.Memory.PromptCacheTTLMinutes)
	}
}

func TestDefaultsMemoryCompactionStrategy(t *testing.T) {
	cfg := Defaults()
	valid := map[string]bool{"rolling": true, "facts": true, "topics": true}
	if !valid[cfg.Memory.CompactionStrategy] {
		t.Fatalf("unexpected CompactionStrategy %q", cfg.Memory.CompactionStrategy)
	}
}

func TestDefaultsSessionsPositiveValues(t *testing.T) {
	cfg := Defaults()
	if cfg.Sessions.MaxConcurrent <= 0 {
		t.Fatalf("MaxConcurrent should be positive, got %d", cfg.Sessions.MaxConcurrent)
	}
	if cfg.Sessions.MaxPerUser <= 0 {
		t.Fatalf("MaxPerUser should be positive, got %d", cfg.Sessions.MaxPerUser)
	}
	if cfg.Sessions.TimeoutMinutes <= 0 {
		t.Fatalf("TimeoutMinutes should be positive, got %d", cfg.Sessions.TimeoutMinutes)
	}
	if cfg.Sessions.MaxTokenBudget <= 0 {
		t.Fatalf("MaxTokenBudget should be positive, got %d", cfg.Sessions.MaxTokenBudget)
	}
	if cfg.Sessions.MaxToolCalls <= 0 {
		t.Fatalf("MaxToolCalls should be positive, got %d", cfg.Sessions.MaxToolCalls)
	}
	if cfg.Sessions.StuckThreshold <= 0 {
		t.Fatalf("StuckThreshold should be positive, got %d", cfg.Sessions.StuckThreshold)
	}
}

func TestDefaultsHITLTimeoutAction(t *testing.T) {
	cfg := Defaults()
	if cfg.HITL.TimeoutAction != "deny" {
		t.Fatalf("expected HITL.TimeoutAction %q, got %q", "deny", cfg.HITL.TimeoutAction)
	}
	if cfg.HITL.TimeoutSeconds <= 0 {
		t.Fatalf("HITL.TimeoutSeconds should be positive, got %d", cfg.HITL.TimeoutSeconds)
	}
}

func TestDefaultsHITLAlwaysPauseToolsNonEmpty(t *testing.T) {
	cfg := Defaults()
	if len(cfg.HITL.AlwaysPauseTools) == 0 {
		t.Fatal("HITL.AlwaysPauseTools should not be empty")
	}
}

func TestDefaultsSecurityBlockedEnvPrefixes(t *testing.T) {
	cfg := Defaults()
	if len(cfg.Security.BlockedEnvPrefixes) == 0 {
		t.Fatal("SECURITY: BlockedEnvPrefixes should not be empty")
	}
	// Must block common secret prefixes
	prefixes := cfg.Security.BlockedEnvPrefixes
	mustBlock := []string{"ANTHROPIC_", "OPENAI_"}
	for _, mb := range mustBlock {
		found := false
		for _, p := range prefixes {
			if p == mb {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("SECURITY: BlockedEnvPrefixes missing %q", mb)
		}
	}
}

func TestDefaultsSecurityBlockedCIDRsIncludePrivateRanges(t *testing.T) {
	cfg := Defaults()
	if len(cfg.Security.BlockedCIDRs) == 0 {
		t.Fatal("SECURITY: BlockedCIDRs should not be empty")
	}
	mustBlock := []string{"127.0.0.0/8", "10.0.0.0/8", "192.168.0.0/16"}
	for _, cidr := range mustBlock {
		found := false
		for _, c := range cfg.Security.BlockedCIDRs {
			if c == cidr {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("SECURITY: BlockedCIDRs missing %q", cidr)
		}
	}
}

func TestDefaultsSecurityInjectionEnabled(t *testing.T) {
	cfg := Defaults()
	if !cfg.Security.Injection.EnableProtection {
		t.Fatal("SECURITY: injection protection should be enabled by default")
	}
}

func TestDefaultsLoggingLevel(t *testing.T) {
	cfg := Defaults()
	if cfg.Logging.Level == "" {
		t.Fatal("Logging.Level should not be empty")
	}
}

func TestDefaultsSkillDirsNonEmpty(t *testing.T) {
	cfg := Defaults()
	if len(cfg.Skills.SkillDirs) == 0 {
		t.Fatal("Skills.SkillDirs should not be empty")
	}
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("Load should not error on missing file: %v", err)
	}
	defaults := Defaults()
	if cfg.Pi.Binary != defaults.Pi.Binary {
		t.Fatalf("expected default Pi.Binary %q, got %q", defaults.Pi.Binary, cfg.Pi.Binary)
	}
	if cfg.Sessions.MaxConcurrent != defaults.Sessions.MaxConcurrent {
		t.Fatalf("expected default MaxConcurrent %d, got %d", defaults.Sessions.MaxConcurrent, cfg.Sessions.MaxConcurrent)
	}
}

func TestLoadValidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "agentloop.yaml")
	content := `
server:
  socket_path: /tmp/test.sock
pi:
  binary: /usr/local/bin/pi
  provider: openai
  model: gpt-4
sessions:
  max_concurrent: 10
logging:
  level: debug
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Server.SocketPath != "/tmp/test.sock" {
		t.Fatalf("expected socket_path %q, got %q", "/tmp/test.sock", cfg.Server.SocketPath)
	}
	if cfg.Pi.Binary != "/usr/local/bin/pi" {
		t.Fatalf("expected pi binary %q, got %q", "/usr/local/bin/pi", cfg.Pi.Binary)
	}
	if cfg.Pi.Provider != "openai" {
		t.Fatalf("expected provider %q, got %q", "openai", cfg.Pi.Provider)
	}
	if cfg.Sessions.MaxConcurrent != 10 {
		t.Fatalf("expected MaxConcurrent 10, got %d", cfg.Sessions.MaxConcurrent)
	}
	if cfg.Logging.Level != "debug" {
		t.Fatalf("expected log level %q, got %q", "debug", cfg.Logging.Level)
	}
}

func TestLoadResolvesHomePaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "agentloop.yaml")
	content := `
vault:
  path: "~/my-vault"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "my-vault")
	if cfg.Vault.Path != expected {
		t.Fatalf("expected vault path %q, got %q", expected, cfg.Vault.Path)
	}
}

func TestLoadResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "agentloop.yaml")
	content := `
vault:
  path: "./data/vault"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	expected := filepath.Join(dir, "data/vault")
	if cfg.Vault.Path != expected {
		t.Fatalf("expected vault path %q, got %q", expected, cfg.Vault.Path)
	}
}

func TestDefaultConfigPathNotEmpty(t *testing.T) {
	p := DefaultConfigPath()
	if p == "" {
		t.Fatal("DefaultConfigPath should not be empty")
	}
}
