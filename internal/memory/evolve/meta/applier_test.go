package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

func TestApplierAgentsMDMarkerProtection(t *testing.T) {
	dir := t.TempDir()
	agentsMD := filepath.Join(dir, "AGENTS.md")

	original := "# Agent Instructions\n\nSome important rules here.\n\n<!-- EVOLVED:START -->\n## Learned Patterns\n\n- Old pattern\n<!-- EVOLVED:END -->\n\nMore content after.\n"
	os.WriteFile(agentsMD, []byte(original), 0644)

	a := NewApplier(dir, agentsMD, dir)
	err := a.ApplyAgentsMD("- New evolved pattern\n- Another pattern")
	if err != nil {
		t.Fatalf("ApplyAgentsMD failed: %v", err)
	}

	data, _ := os.ReadFile(agentsMD)
	content := string(data)

	if !strings.Contains(content, "Some important rules here.") {
		t.Fatal("SECURITY: content outside markers was modified")
	}
	if !strings.Contains(content, "More content after.") {
		t.Fatal("SECURITY: content after markers was modified")
	}
	if !strings.Contains(content, "New evolved pattern") {
		t.Fatal("evolved content not inserted")
	}
	if strings.Contains(content, "Old pattern") {
		t.Fatal("old evolved content should be replaced")
	}
}

func TestApplierAgentsMDCreatesMarkers(t *testing.T) {
	dir := t.TempDir()
	agentsMD := filepath.Join(dir, "AGENTS.md")

	original := "# Agent Instructions\n\nSome rules.\n"
	os.WriteFile(agentsMD, []byte(original), 0644)

	a := NewApplier(dir, agentsMD, dir)
	err := a.ApplyAgentsMD("- Learned something")
	if err != nil {
		t.Fatalf("ApplyAgentsMD failed: %v", err)
	}

	data, _ := os.ReadFile(agentsMD)
	content := string(data)

	if !strings.Contains(content, "<!-- EVOLVED:START -->") {
		t.Fatal("markers should be created")
	}
	if !strings.Contains(content, "Learned something") {
		t.Fatal("evolved content not inserted")
	}
	if !strings.Contains(content, "Some rules.") {
		t.Fatal("original content should be preserved")
	}
}

func TestApplierSkillNamespacing(t *testing.T) {
	dir := t.TempDir()
	a := NewApplier(dir, "", dir)

	err := a.ApplySkill(SkillProposal{
		Action:      "create",
		Name:        "evolved-auth-patterns",
		Content:     "# Auth Patterns\n\nSome instructions.",
		Triggers:    []string{"auth", "token"},
		Description: "Auth handling improvements",
	})
	if err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}

	err = a.ApplySkill(SkillProposal{
		Action: "create",
		Name:   "auth-patterns",
	})
	if err == nil {
		t.Fatal("SECURITY: skill without evolved- prefix should be rejected")
	}
}

func TestApplierSnapshotCreated(t *testing.T) {
	dir := t.TempDir()
	evolvedDir := filepath.Join(dir, "memory", "evolved")
	os.MkdirAll(evolvedDir, 0755)

	configPath := filepath.Join(evolvedDir, "pipeline-config.yaml")
	os.WriteFile(configPath, []byte("version: 1\n"), 0644)

	a := NewApplier(dir, "", dir)
	ts, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	snapshotDir := filepath.Join(evolvedDir, "snapshots", ts)
	if _, err := os.Stat(snapshotDir); os.IsNotExist(err) {
		t.Fatal("snapshot directory not created")
	}
}

func TestApplierConfigWrite(t *testing.T) {
	dir := t.TempDir()
	evolvedDir := filepath.Join(dir, "memory", "evolved")
	os.MkdirAll(evolvedDir, 0755)

	a := NewApplier(dir, "", dir)
	cfg := evolve.DefaultPipelineConfig()
	cfg.Version = 5
	cfg.Retriever.MaxResults = 15

	err := a.ApplyConfig(cfg)
	if err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	loaded, err := evolve.LoadPipelineConfig(filepath.Join(evolvedDir, "pipeline-config.yaml"))
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Version != 5 {
		t.Fatalf("expected version 5, got %d", loaded.Version)
	}
}

func TestApplierGitNotInstalled(t *testing.T) {
	dir := t.TempDir()
	a := NewApplier(dir, "", dir)
	a.gitCommit("test commit that should be skipped or init")
}

func TestProposalParsingInvalid(t *testing.T) {
	_, err := ParseProposal("this is not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}

	_, err = ParseProposal("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestProposalParsingValid(t *testing.T) {
	input := `Here is my proposal:
{"reasoning":"auth failures","config_changes":{"version":2},"skill_changes":[],"agents_md_patch":"","summary":"tune auth retrieval"}
Done.`
	p, err := ParseProposal(input)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if p.Summary != "tune auth retrieval" {
		t.Fatalf("unexpected summary: %s", p.Summary)
	}
}
