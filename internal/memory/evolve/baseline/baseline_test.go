package baseline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

func TestBaselineEncoderWrapsExisting(t *testing.T) {
	dir := t.TempDir()
	profiles := memory.NewProfileStore(dir)
	encoder := NewBaselineEncoder(profiles)

	out, err := encoder.Encode(context.Background(), evolve.EncoderInput{
		UserID:      "test-user",
		UserMessage: "fix the authentication token refresh bug",
		AgentReply:  "I'll check the auth module",
		ToolsUsed:   []string{"bash", "edit"},
	})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if len(out.Units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(out.Units))
	}
	if out.Units[0].Role != "user" {
		t.Fatalf("expected user role, got %s", out.Units[0].Role)
	}
	if out.Units[1].Role != "assistant" {
		t.Fatalf("expected assistant role, got %s", out.Units[1].Role)
	}
	if len(out.Units[0].Keywords) == 0 {
		t.Fatal("expected keywords to be extracted")
	}
	// Verify userId is in metadata (plan spec requirement)
	if out.Units[0].Metadata["userId"] != "test-user" {
		t.Fatalf("expected userId in metadata, got %s", out.Units[0].Metadata["userId"])
	}
}

func TestBaselineManagerWrapsCompactor(t *testing.T) {
	compactor := memory.NewCompactor("rolling")
	manager := NewBaselineManager(compactor)

	input := evolve.CompactionInput{
		Text:      "### 10:00:00 [user]\nfix auth bug\n\n### 10:01:00 [assistant]\nI'll check the auth module\n\n### 10:02:00 [user]\nalso check the database\n\n### 10:03:00 [assistant]\nChecking database now\n\n### 10:04:00 [user]\nlooks good\n\n### 10:05:00 [assistant]\nDone!\n\n### 10:06:00 [user]\nnow deploy\n\n### 10:07:00 [assistant]\nDeploying...\n",
		MaxTokens: 50,
		Strategy:  "rolling",
	}

	result, err := manager.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if result.Text == "" {
		t.Fatal("expected non-empty compacted text")
	}
}

func TestBaselineRetrieverWrapsExisting(t *testing.T) {
	dir := t.TempDir()
	profiles := memory.NewProfileStore(dir)
	conversations := memory.NewConversationStore(dir, 30)

	conversations.Append("test-user", "user", "fix the authentication token refresh bug", "")
	conversations.Append("test-user", "assistant", "I'll check the auth module and fix the token refresh", "")

	cfg := &evolve.RetrieverConfig{
		Strategy:       "jaccard",
		MaxResults:     8,
		FallbackRecent: 5,
		TopicBonus:     0.2,
		Position:       "edges",
	}
	retriever := NewBaselineRetriever(profiles, conversations, cfg)

	result, err := retriever.Retrieve(context.Background(), evolve.RetrievalQuery{
		UserID: "test-user",
		Task:   "fix auth token",
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if result.Context == "" {
		t.Fatal("expected non-empty context")
	}
}

func TestPipelineReloadAtomicSwap(t *testing.T) {
	dir := t.TempDir()
	evolvedDir := filepath.Join(dir, "memory", "evolved")
	os.MkdirAll(evolvedDir, 0755)

	defaults := evolve.DefaultPipelineConfig()
	profiles := memory.NewProfileStore(dir)
	conversations := memory.NewConversationStore(dir, 30)
	compactor := memory.NewCompactor("rolling")

	encoder := NewBaselineEncoder(profiles)
	storer := NewBaselineStorer(profiles, conversations)
	retriever := NewBaselineRetriever(profiles, conversations, &defaults.Retriever)
	manager := NewBaselineManager(compactor)

	holder := evolve.NewPipelineHolder(dir, defaults, encoder, storer, retriever, manager)

	evolved := evolve.DefaultPipelineConfig()
	evolved.Version = 42
	evolved.Retriever.MaxResults = 20
	evolve.SavePipelineConfig(filepath.Join(evolvedDir, "pipeline-config.yaml"), evolved)

	before := holder.Get()

	if err := holder.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if before == nil {
		t.Fatal("old pipeline reference should still be valid")
	}

	after := holder.Get()
	if after.Config().Version != 42 {
		t.Fatalf("expected version 42, got %d", after.Config().Version)
	}
}
