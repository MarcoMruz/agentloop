package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/notes"
)

func TestApplierAgentsMDMarkerProtection(t *testing.T) {
	dir := t.TempDir()
	agentsMD := filepath.Join(dir, "AGENTS.md")

	original := "# Agent Instructions\n\nSome important rules here.\n\n<!-- EVOLVED:START -->\n## Learned Patterns\n\n- Old pattern\n<!-- EVOLVED:END -->\n\nMore content after.\n"
	os.WriteFile(agentsMD, []byte(original), 0644)

	a := NewApplier(dir, agentsMD, dir, nil)
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

	a := NewApplier(dir, agentsMD, dir, nil)
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
	a := NewApplier(dir, "", dir, nil)

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

	a := NewApplier(dir, "", dir, nil)
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

	a := NewApplier(dir, "", dir, nil)
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
	a := NewApplier(dir, "", dir, nil)
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

func TestApplierOrchestratorPatchDefaultAgent(t *testing.T) {
	dir := t.TempDir()
	a := NewApplier(dir, "", dir, nil)

	patch := OrchestratorPatch{
		Role:    "worker",
		Content: "## Worker Instructions\n\nDo the work carefully.",
	}
	err := a.ApplyOrchestratorPatch(patch)
	if err != nil {
		t.Fatalf("ApplyOrchestratorPatch failed: %v", err)
	}

	path := filepath.Join(dir, "agents", "worker-evolved.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	content := string(data)

	if !strings.Contains(content, "role: worker") {
		t.Fatal("expected role in frontmatter")
	}
	if !strings.Contains(content, "Worker Instructions") {
		t.Fatal("expected content body in file")
	}
}

func TestApplierOrchestratorPatchProjectAgent(t *testing.T) {
	dir := t.TempDir()
	customDir := filepath.Join(dir, "custom-agents")
	a := NewApplier(dir, "", dir, nil)

	patch := OrchestratorPatch{
		Role:    "planner",
		Content: "## Planner Instructions\n\nPlan carefully.",
	}
	err := a.ApplyOrchestratorPatchToDir(customDir, patch)
	if err != nil {
		t.Fatalf("ApplyOrchestratorPatchToDir failed: %v", err)
	}

	path := filepath.Join(customDir, "planner-evolved.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	content := string(data)

	if !strings.Contains(content, "role: planner") {
		t.Fatal("expected role in frontmatter")
	}
	if !strings.Contains(content, "Planner Instructions") {
		t.Fatal("expected content body in file")
	}
}

func TestApplierOrchestratorPatchInvalidRole(t *testing.T) {
	dir := t.TempDir()
	a := NewApplier(dir, "", dir, nil)

	patch := OrchestratorPatch{
		Role:    "hacker",
		Content: "Do evil things.",
	}
	err := a.ApplyOrchestratorPatch(patch)
	if err == nil {
		t.Fatal("expected error for invalid role 'hacker'")
	}
}

func TestProposalParsingValid(t *testing.T) {
	input := `Here is my proposal:
{"reasoning":"auth failures","config_changes":{"version":2},"skill_changes":[],"agents_md_patch":"","note_proposals":[],"summary":"tune auth retrieval"}
Done.`
	p, err := ParseProposal(input)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if p.Summary != "tune auth retrieval" {
		t.Fatalf("unexpected summary: %s", p.Summary)
	}
}

func TestApplierNoteProposals(t *testing.T) {
	dir := t.TempDir()
	store, err := notes.NewSQLiteNoteStore(filepath.Join(dir, "notes"), 0)
	if err != nil {
		t.Fatalf("NewSQLiteNoteStore: %v", err)
	}
	defer store.Close()

	engine := memory.NewEngine(dir, 4000, "rolling", 30)
	engine.SetNoteStore(store)

	a := NewApplier(dir, "", dir, engine)
	proposals := []NoteProposal{
		{
			Content:     "When OAuth2 token refresh fails, retry with exponential backoff starting at 1s. Log each failure with the HTTP status code.",
			Keywords:    []string{"oauth2", "token", "refresh", "backoff"},
			Tags:        []string{"auth", "security"},
			Description: "OAuth2 token refresh: retry with exponential backoff, log HTTP status",
		},
	}

	if err := a.ApplyNoteProposals(proposals, "test-user"); err != nil {
		t.Fatalf("ApplyNoteProposals failed: %v", err)
	}

	results, err := store.SearchByKeywords("test-user", []string{"oauth2", "token"}, 5)
	if err != nil {
		t.Fatalf("SearchByKeywords failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected note to be retrievable by keyword")
	}
	if !strings.Contains(results[0].Content, "exponential backoff") {
		t.Fatalf("unexpected note content: %s", results[0].Content)
	}
}
