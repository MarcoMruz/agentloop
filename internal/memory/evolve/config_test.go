package evolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPipelineConfigLoad(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "pipeline-config.yaml")

	yaml := `version: 2
updated: "2026-03-15T14:00:00Z"
evolved_from: "sess-abc123"
encoder:
  strategy: "baseline"
  keyword_limit: 20
retriever:
  strategy: "jaccard"
  max_results: 10
  topic_bonus: 0.3
  position: "edges"
manager:
  strategy: "facts"
  facts_max: 50
storer:
  format: "markdown"
  index_sidecar: true
`
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadPipelineConfig(configPath)
	if err != nil {
		t.Fatalf("LoadPipelineConfig failed: %v", err)
	}
	if cfg.Version != 2 {
		t.Fatalf("expected version 2, got %d", cfg.Version)
	}
	if cfg.Encoder.KeywordLimit != 20 {
		t.Fatalf("expected keyword_limit 20, got %d", cfg.Encoder.KeywordLimit)
	}
	if cfg.Retriever.MaxResults != 10 {
		t.Fatalf("expected max_results 10, got %d", cfg.Retriever.MaxResults)
	}
	if cfg.Retriever.Position != "edges" {
		t.Fatalf("expected position edges, got %s", cfg.Retriever.Position)
	}
	if cfg.Manager.Strategy != "facts" {
		t.Fatalf("expected strategy facts, got %s", cfg.Manager.Strategy)
	}
}

func TestPipelineConfigMergeDefaults(t *testing.T) {
	defaults := DefaultPipelineConfig()
	evolved := &PipelineConfig{
		Retriever: RetrieverConfig{
			MaxResults: 12,
			TopicBonus: 0.4,
		},
	}

	merged := MergeWithDefaults(defaults, evolved)
	if merged.Retriever.MaxResults != 12 {
		t.Fatalf("expected evolved max_results 12, got %d", merged.Retriever.MaxResults)
	}
	if merged.Retriever.TopicBonus != 0.4 {
		t.Fatalf("expected evolved topic_bonus 0.4, got %f", merged.Retriever.TopicBonus)
	}
	if merged.Encoder.Strategy != "baseline" {
		t.Fatalf("expected default encoder strategy, got %s", merged.Encoder.Strategy)
	}
	if merged.Retriever.Position != "edges" {
		t.Fatalf("position must always be edges, got %s", merged.Retriever.Position)
	}
}

func TestPipelineConfigLoadMissing(t *testing.T) {
	cfg, err := LoadPipelineConfig("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg != nil {
		t.Fatal("missing file should return nil config")
	}
}
