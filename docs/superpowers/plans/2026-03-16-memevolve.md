# MemEvolve Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace AgentLoop's static memory engine with a self-evolving architecture that learns from task outcomes and autonomously improves retrieval, encoding, compaction, and agent behavior.

**Architecture:** Two parallel tracks converging: Track 1 extracts four interfaces (Encoder, Storer, Retriever, Manager) from the existing memory engine with baseline wrappers. Track 2 builds the meta-evolution infrastructure (metrics collection, topic clustering, dedicated pi meta-agent, change applier). Both tracks share a Pipeline orchestrator and PipelineConfig YAML format.

**Tech Stack:** Go 1.23, existing deps only (uuid, yaml, viper). Pi subprocess for meta-agent LLM calls. JSONL for metrics. YAML for pipeline config. Markdown for evolved skills.

**Spec:** `docs/superpowers/specs/2026-03-15-memevolve-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/memory/evolve/interfaces.go` | Four interfaces + MemoryUnit + input/output/filter types |
| `internal/memory/evolve/pipeline.go` | Pipeline orchestrator with atomic pointer swap |
| `internal/memory/evolve/config.go` | PipelineConfig struct, YAML load/save, mergeWithDefaults |
| `internal/memory/evolve/config_test.go` | Config loading and merge tests |
| `internal/memory/evolve/baseline/encoder.go` | BaselineEncoder wrapping existing heuristic extraction |
| `internal/memory/evolve/baseline/storer.go` | BaselineStorer wrapping ProfileStore + ConversationStore |
| `internal/memory/evolve/baseline/retriever.go` | BaselineRetriever wrapping Jaccard + topic scoring |
| `internal/memory/evolve/baseline/manager.go` | BaselineManager wrapping three compaction strategies |
| `internal/memory/evolve/baseline/baseline_test.go` | Tests verifying baseline wrappers match existing behavior |
| `internal/memory/evolve/metrics/outcome.go` | TaskOutcome struct + Score() |
| `internal/memory/evolve/metrics/outcome_test.go` | Score calculation tests |
| `internal/memory/evolve/metrics/collector.go` | Collector: JSONL persistence, rate limiting, triggers meta-agent |
| `internal/memory/evolve/metrics/collector_test.go` | Collector + rate limiting tests |
| `internal/memory/evolve/metrics/cluster.go` | Topic-scoped outcome clustering |
| `internal/memory/evolve/metrics/cluster_test.go` | Clustering tests |
| `internal/memory/evolve/meta/proposal.go` | EvolutionProposal + SkillProposal structs |
| `internal/memory/evolve/meta/prompt.go` | EvolutionPrompt template construction |
| `internal/memory/evolve/meta/applier.go` | Applies proposals: config, skills, AGENTS.md, git, snapshot |
| `internal/memory/evolve/meta/applier_test.go` | Applier tests (marker protection, namespacing, git) |
| `internal/memory/evolve/meta/agent.go` | MetaAgent: pi session lifecycle, mutex, rate limiter |
| `internal/memory/evolve/version/snapshot.go` | Snapshot current state to timestamped directory |
| `internal/memory/evolve/version/log.go` | Evolution log (JSONL append) |
| `internal/memory/evolve/version/version_test.go` | Snapshot + log tests |

### Modified Files

| File | Change |
|------|--------|
| `internal/memory/index.go:105-154` | Export `extractKeywords` → `ExtractKeywords`, `extractTopics` → `ExtractTopics` |
| `internal/memory/engine.go:10-16` | Add `pipeline` field, delegate methods when pipeline != nil |
| `internal/session/session.go:33-53` | Add `hitlDenials int32`, `steerCount int32` fields + accessor methods |
| `internal/session/session.go:137-150` | Increment denial counter in `ResolveHITL()` |
| `internal/session/session.go:105-112` | Increment steer counter in `Steer()` |
| `internal/session/manager.go:161-174` | Call metrics collector after task completion |
| `internal/config/config.go:3-13` | Add `Evolution EvolutionConfig` field to Config struct |
| `internal/config/config.go:103-257` | Add EvolutionConfig defaults in `Defaults()` |
| `internal/config/loader.go:38-43` | Add `expandHome` for `PipelineConfigPath` |
| `configs/agentloop.yaml` | Add `evolution:` section |

---

## Chunk 1: Foundation — Interfaces, Types, and Config

### Task 1: Create MemEvolve interfaces and types

**Files:**
- Create: `internal/memory/evolve/interfaces.go`

- [ ] **Step 1: Create the evolve package with interfaces and types**

```go
// internal/memory/evolve/interfaces.go
package evolve

import (
	"context"
	"time"
)

// Encoder transforms raw interactions into structured memory units.
type Encoder interface {
	Encode(ctx context.Context, input EncoderInput) (*EncoderOutput, error)
}

// Storer persists and loads memory units.
type Storer interface {
	Store(ctx context.Context, units []MemoryUnit) error
	Load(ctx context.Context, userId string, filter StoreFilter) ([]MemoryUnit, error)
}

// Retriever selects relevant memory for a given task context.
type Retriever interface {
	Retrieve(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error)
}

// Manager consolidates and prunes stored memory under a token budget.
type Manager interface {
	Compact(ctx context.Context, input CompactionInput) (*CompactionResult, error)
}

// MemoryUnit is the universal exchange type between interfaces.
type MemoryUnit struct {
	ID        string
	Timestamp time.Time
	Role      string            // "user", "assistant", "system"
	Content   string
	Keywords  []string
	Topics    []string
	Metadata  map[string]string // Extensible: contextID, source, toolsUsed, type
	Score     float64           // Set by Retriever during scoring
}

// EncoderInput holds raw interaction data for encoding.
type EncoderInput struct {
	UserID                string
	UserMessage           string
	AgentReply            string
	ToolsUsed             []string
	ConversationContextID string
}

// EncoderOutput holds the encoded memory units.
type EncoderOutput struct {
	Units []MemoryUnit
}

// StoreFilter controls what Load() returns.
type StoreFilter struct {
	Type      string // "profile", "conversation", "all"
	ContextID string // Filter by conversation context ID (empty = no filter)
	MaxItems  int
}

// RetrievalQuery describes what memory to retrieve.
type RetrievalQuery struct {
	UserID    string
	Task      string
	ContextID string // For thread-scoped retrieval
	MaxTokens int
}

// RetrievalResult holds retrieved memory formatted for prompt injection.
type RetrievalResult struct {
	Context       string // Assembled text for <memory> block
	TokenEstimate int
	UnitsUsed     int
}

// CompactionInput holds data for compaction.
type CompactionInput struct {
	Text      string
	MaxTokens int
	Strategy  string
}

// CompactionResult from the Manager.
type CompactionResult struct {
	Text           string
	TokenEstimate  int
	EntriesRemoved int
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/...`
Expected: Success, no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/interfaces.go
git commit -m "feat(evolve): add MemEvolve interfaces and types"
```

---

### Task 2: Create PipelineConfig and YAML loading

**Files:**
- Create: `internal/memory/evolve/config.go`
- Create: `internal/memory/evolve/config_test.go`

- [ ] **Step 1: Write the failing test for config loading**

```go
// internal/memory/evolve/config_test.go
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
	// Defaults preserved for unset fields
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/evolve/... -run TestPipelineConfig -v`
Expected: FAIL — `LoadPipelineConfig` not defined.

- [ ] **Step 3: Write PipelineConfig and loading**

```go
// internal/memory/evolve/config.go
package evolve

import (
	"os"

	"gopkg.in/yaml.v3"
)

// PipelineConfig is the declarative configuration for the memory pipeline.
type PipelineConfig struct {
	Version     int             `yaml:"version"`
	Updated     string          `yaml:"updated"`
	EvolvedFrom string          `yaml:"evolved_from"`
	Encoder     EncoderConfig   `yaml:"encoder"`
	Retriever   RetrieverConfig `yaml:"retriever"`
	Manager     ManagerConfig   `yaml:"manager"`
	Storer      StorerConfig    `yaml:"storer"`
}

type EncoderConfig struct {
	Strategy               string   `yaml:"strategy"`
	KeywordLimit           int      `yaml:"keyword_limit"`
	TopicTaxonomyExtensions []string `yaml:"topic_taxonomy_extensions"`
	ExtractToolPatterns    bool     `yaml:"extract_tool_patterns"`
}

type RetrieverConfig struct {
	Strategy       string  `yaml:"strategy"`
	MaxResults     int     `yaml:"max_results"`
	FallbackRecent int     `yaml:"fallback_recent"`
	TopicBonus     float64 `yaml:"topic_bonus"`
	RecencyWeight  float64 `yaml:"recency_weight"`
	Position       string  `yaml:"position"`
}

type ManagerConfig struct {
	Strategy          string   `yaml:"strategy"`
	RollingKeepRecent int      `yaml:"rolling_keep_recent"`
	FactsMax          int      `yaml:"facts_max"`
	FactsKeywords     []string `yaml:"facts_keywords"`
	AggressiveFilter  bool     `yaml:"aggressive_filter"`
}

type StorerConfig struct {
	Format           string `yaml:"format"`
	IndexSidecar     bool   `yaml:"index_sidecar"`
	ContextIsolation bool   `yaml:"context_isolation"`
}

// DefaultPipelineConfig returns baseline configuration matching existing behavior.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		Version: 1,
		Encoder: EncoderConfig{
			Strategy:     "baseline",
			KeywordLimit: 15,
		},
		Retriever: RetrieverConfig{
			Strategy:       "jaccard",
			MaxResults:     8,
			FallbackRecent: 5,
			TopicBonus:     0.2,
			Position:       "edges",
		},
		Manager: ManagerConfig{
			Strategy:          "rolling",
			RollingKeepRecent: 5,
			FactsMax:          30,
			FactsKeywords: []string{
				"decided", "confirmed", "created", "fixed", "deployed",
				"merged", "installed", "configured", "updated", "deleted",
				"error", "failed",
			},
		},
		Storer: StorerConfig{
			Format:           "markdown",
			IndexSidecar:     true,
			ContextIsolation: true,
		},
	}
}

// LoadPipelineConfig reads a pipeline config from YAML. Returns nil, nil if file missing.
func LoadPipelineConfig(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg PipelineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SavePipelineConfig writes pipeline config to YAML.
func SavePipelineConfig(path string, cfg *PipelineConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// MergeWithDefaults overlays evolved config onto defaults.
// Non-zero evolved values override defaults. Position is always forced to "edges".
func MergeWithDefaults(defaults, evolved *PipelineConfig) *PipelineConfig {
	merged := *defaults

	if evolved == nil {
		return &merged
	}

	// Encoder
	if evolved.Encoder.Strategy != "" {
		merged.Encoder.Strategy = evolved.Encoder.Strategy
	}
	if evolved.Encoder.KeywordLimit != 0 {
		merged.Encoder.KeywordLimit = evolved.Encoder.KeywordLimit
	}
	if len(evolved.Encoder.TopicTaxonomyExtensions) > 0 {
		merged.Encoder.TopicTaxonomyExtensions = evolved.Encoder.TopicTaxonomyExtensions
	}
	if evolved.Encoder.ExtractToolPatterns {
		merged.Encoder.ExtractToolPatterns = true
	}

	// Retriever
	if evolved.Retriever.Strategy != "" {
		merged.Retriever.Strategy = evolved.Retriever.Strategy
	}
	if evolved.Retriever.MaxResults != 0 {
		merged.Retriever.MaxResults = evolved.Retriever.MaxResults
	}
	if evolved.Retriever.FallbackRecent != 0 {
		merged.Retriever.FallbackRecent = evolved.Retriever.FallbackRecent
	}
	if evolved.Retriever.TopicBonus != 0 {
		merged.Retriever.TopicBonus = evolved.Retriever.TopicBonus
	}
	if evolved.Retriever.RecencyWeight != 0 {
		merged.Retriever.RecencyWeight = evolved.Retriever.RecencyWeight
	}
	// Position is always "edges" — hard constraint, not overridable
	merged.Retriever.Position = "edges"

	// Manager
	if evolved.Manager.Strategy != "" {
		merged.Manager.Strategy = evolved.Manager.Strategy
	}
	if evolved.Manager.RollingKeepRecent != 0 {
		merged.Manager.RollingKeepRecent = evolved.Manager.RollingKeepRecent
	}
	if evolved.Manager.FactsMax != 0 {
		merged.Manager.FactsMax = evolved.Manager.FactsMax
	}
	if len(evolved.Manager.FactsKeywords) > 0 {
		merged.Manager.FactsKeywords = evolved.Manager.FactsKeywords
	}
	// AggressiveFilter is a bool — always take evolved value if config was loaded
	merged.Manager.AggressiveFilter = evolved.Manager.AggressiveFilter

	// Storer
	if evolved.Storer.Format != "" {
		merged.Storer.Format = evolved.Storer.Format
	}
	merged.Storer.IndexSidecar = evolved.Storer.IndexSidecar
	merged.Storer.ContextIsolation = evolved.Storer.ContextIsolation

	// Metadata
	if evolved.Version != 0 {
		merged.Version = evolved.Version
	}
	if evolved.Updated != "" {
		merged.Updated = evolved.Updated
	}
	if evolved.EvolvedFrom != "" {
		merged.EvolvedFrom = evolved.EvolvedFrom
	}

	return &merged
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/evolve/... -run TestPipelineConfig -v`
Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/evolve/config.go internal/memory/evolve/config_test.go
git commit -m "feat(evolve): add PipelineConfig with YAML loading and merge"
```

---

### Task 3: Add EvolutionConfig to server config

**Files:**
- Modify: `internal/config/config.go:3-13` (Config struct) and `:103-257` (Defaults)
- Modify: `internal/config/loader.go:38-43` (path expansion)
- Modify: `configs/agentloop.yaml` (add evolution section)

- [ ] **Step 1: Add EvolutionConfig struct and field to Config**

In `internal/config/config.go`, add after the existing config structs (before `Defaults()`):

```go
type EvolutionConfig struct {
	Enabled            bool    `mapstructure:"enabled"`
	ScoreThreshold     float64 `mapstructure:"score_threshold"`
	MetaTokenBudget    int     `mapstructure:"meta_token_budget"`
	MaxOutcomesPerRun  int     `mapstructure:"max_outcomes_per_run"`
	SnapshotRetainMax  int     `mapstructure:"snapshot_retain_max"`
	PipelineConfigPath string  `mapstructure:"pipeline_config_path"`
	MinCooldownSeconds int     `mapstructure:"min_cooldown_seconds"`
	MaxDailyRuns       int     `mapstructure:"max_daily_runs"`
}
```

Add to `Config` struct:
```go
Evolution EvolutionConfig `mapstructure:"evolution"`
```

- [ ] **Step 2: Add defaults in Defaults()**

In `Defaults()` function, add:
```go
Evolution: EvolutionConfig{
	Enabled:            true,
	ScoreThreshold:     0.7,
	MetaTokenBudget:    10000,
	MaxOutcomesPerRun:  10,
	SnapshotRetainMax:  50,
	PipelineConfigPath: "",
	MinCooldownSeconds: 300,
	MaxDailyRuns:       10,
},
```

- [ ] **Step 3: Add path expansion in loader.go**

In `internal/config/loader.go`, after the existing `resolvePath` calls (around line 40-43), add:
```go
cfg.Evolution.PipelineConfigPath = resolvePath(cfg.Evolution.PipelineConfigPath, configDir)
```

- [ ] **Step 4: Add evolution section to configs/agentloop.yaml**

Append to end of `configs/agentloop.yaml`:
```yaml

# Evolution (MemEvolve)
evolution:
  enabled: true
  score_threshold: 0.7
  meta_token_budget: 10000
  max_outcomes_per_run: 10
  snapshot_retain_max: 50
  min_cooldown_seconds: 300
  max_daily_runs: 10
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: Success.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/loader.go configs/agentloop.yaml
git commit -m "feat(config): add EvolutionConfig for MemEvolve"
```

---

### Task 4: Export keyword/topic extraction from index.go

**Files:**
- Modify: `internal/memory/index.go:105-154`

- [ ] **Step 1: Rename extractKeywords to ExtractKeywords**

In `internal/memory/index.go`, rename `func extractKeywords(` to `func ExtractKeywords(` (line 105). Update all callers within the same file (used in `buildIndexEntry` at line 61 and `scoreEntry` at line 79).

- [ ] **Step 2: Rename extractTopics to ExtractTopics**

Rename `func extractTopics(` to `func ExtractTopics(` (line 140). Update all callers within the same file (used in `buildIndexEntry` at line 62 and `scoreEntry` at line 84).

- [ ] **Step 3: Run existing tests**

Run: `go test ./internal/memory/... -v`
Expected: All existing tests still pass — the export is backward-compatible since callers are in the same package.

- [ ] **Step 4: Commit**

```bash
git add internal/memory/index.go
git commit -m "refactor(memory): export ExtractKeywords and ExtractTopics"
```

---

## Chunk 2: Pipeline and Baseline Implementations

### Task 5: Create Pipeline orchestrator

**Files:**
- Create: `internal/memory/evolve/pipeline.go`

- [ ] **Step 1: Write Pipeline with atomic pointer swap**

```go
// internal/memory/evolve/pipeline.go
package evolve

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
)

// Pipeline orchestrates the four memory interfaces.
type Pipeline struct {
	encoder   Encoder
	storer    Storer
	retriever Retriever
	manager   Manager
	config    *PipelineConfig
}

// PipelineHolder manages the active pipeline with atomic swap for hot-reloading.
type PipelineHolder struct {
	active    atomic.Pointer[Pipeline]
	vaultPath string
	defaults  *PipelineConfig
}

// NewPipelineHolder creates a holder, loads evolved config, and builds the initial pipeline.
func NewPipelineHolder(vaultPath string, defaults *PipelineConfig, encoder Encoder, storer Storer, retriever Retriever, manager Manager) *PipelineHolder {
	h := &PipelineHolder{
		vaultPath: vaultPath,
		defaults:  defaults,
	}

	configPath := filepath.Join(vaultPath, "memory", "evolved", "pipeline-config.yaml")
	evolved, err := LoadPipelineConfig(configPath)
	if err != nil {
		slog.Warn("failed to load evolved pipeline config", "error", err)
	}
	cfg := MergeWithDefaults(defaults, evolved)

	p := &Pipeline{
		encoder:   encoder,
		storer:    storer,
		retriever: retriever,
		manager:   manager,
		config:    cfg,
	}
	h.active.Store(p)
	return h
}

// Get returns the current active pipeline.
func (h *PipelineHolder) Get() *Pipeline {
	return h.active.Load()
}

// Reload reads the evolved config and swaps in a new pipeline.
func (h *PipelineHolder) Reload() error {
	configPath := filepath.Join(h.vaultPath, "memory", "evolved", "pipeline-config.yaml")
	evolved, err := LoadPipelineConfig(configPath)
	if err != nil {
		return err
	}
	cfg := MergeWithDefaults(h.defaults, evolved)

	old := h.active.Load()
	p := &Pipeline{
		encoder:   old.encoder,
		storer:    old.storer,
		retriever: old.retriever,
		manager:   old.manager,
		config:    cfg,
	}
	h.active.Store(p)
	slog.Info("pipeline reloaded", "version", cfg.Version)
	return nil
}

// ConfigVersion returns the active config version.
func (h *PipelineHolder) ConfigVersion() string {
	p := h.active.Load()
	if p == nil || p.config == nil {
		return "0"
	}
	return strconv.Itoa(p.config.Version)
}

// Config returns the active pipeline config.
func (p *Pipeline) Config() *PipelineConfig {
	return p.config
}

// Retrieve delegates to the active retriever.
func (p *Pipeline) Retrieve(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error) {
	return p.retriever.Retrieve(ctx, query)
}

// Encode delegates to the active encoder.
func (p *Pipeline) Encode(ctx context.Context, input EncoderInput) (*EncoderOutput, error) {
	return p.encoder.Encode(ctx, input)
}

// Store delegates to the active storer.
func (p *Pipeline) Store(ctx context.Context, units []MemoryUnit) error {
	return p.storer.Store(ctx, units)
}

// Compact delegates to the active manager.
func (p *Pipeline) Compact(ctx context.Context, input CompactionInput) (*CompactionResult, error) {
	return p.manager.Compact(ctx, input)
}

// EnsureEvolvedDirs creates the vault/memory/evolved/ directory structure.
func EnsureEvolvedDirs(vaultPath string) error {
	dirs := []string{
		filepath.Join(vaultPath, "memory", "evolved"),
		filepath.Join(vaultPath, "memory", "evolved", "metrics"),
		filepath.Join(vaultPath, "memory", "evolved", "snapshots"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// Note: uses strconv.Itoa from stdlib — add "strconv" to imports.
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/...`
Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/pipeline.go
git commit -m "feat(evolve): add Pipeline orchestrator with atomic hot-swap"
```

---

### Task 6: Create BaselineEncoder

**Files:**
- Create: `internal/memory/evolve/baseline/encoder.go`

- [ ] **Step 1: Write BaselineEncoder**

```go
// internal/memory/evolve/baseline/encoder.go
package baseline

import (
	"context"
	"time"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineEncoder wraps the existing heuristic extraction from ProfileStore and index.
type BaselineEncoder struct {
	profiles *memory.ProfileStore
}

// NewBaselineEncoder creates an encoder that delegates to existing code.
func NewBaselineEncoder(profiles *memory.ProfileStore) *BaselineEncoder {
	return &BaselineEncoder{profiles: profiles}
}

// Encode extracts keywords, topics, and updates the user profile (existing behavior).
func (e *BaselineEncoder) Encode(ctx context.Context, input evolve.EncoderInput) (*evolve.EncoderOutput, error) {
	// Update profile heuristics (same as existing UpdateFromInteraction)
	e.profiles.UpdateFromInteraction(input.UserID, input.UserMessage, input.ToolsUsed)

	// Build memory units for both user message and agent reply
	now := time.Now()
	units := make([]evolve.MemoryUnit, 0, 2)

	if input.UserMessage != "" {
		units = append(units, evolve.MemoryUnit{
			ID:        input.UserID + "-" + now.Format("150405") + "-user",
			Timestamp: now,
			Role:      "user",
			Content:   input.UserMessage,
			Keywords:  memory.ExtractKeywords(input.UserMessage),
			Topics:    memory.ExtractTopics(input.UserMessage),
			Metadata: map[string]string{
				"type":      "conversation",
				"contextID": input.ConversationContextID,
				"userId":    input.UserID,
			},
		})
	}

	if input.AgentReply != "" {
		units = append(units, evolve.MemoryUnit{
			ID:        input.UserID + "-" + now.Format("150405") + "-assistant",
			Timestamp: now,
			Role:      "assistant",
			Content:   input.AgentReply,
			Keywords:  memory.ExtractKeywords(input.AgentReply),
			Topics:    memory.ExtractTopics(input.AgentReply),
			Metadata: map[string]string{
				"type":      "conversation",
				"contextID": input.ConversationContextID,
				"userId":    input.UserID,
			},
		})
	}

	return &evolve.EncoderOutput{Units: units}, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/baseline/...`
Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/baseline/encoder.go
git commit -m "feat(evolve): add BaselineEncoder wrapping existing heuristics"
```

---

### Task 7: Create BaselineStorer

**Files:**
- Create: `internal/memory/evolve/baseline/storer.go`

- [ ] **Step 1: Write BaselineStorer**

```go
// internal/memory/evolve/baseline/storer.go
package baseline

import (
	"context"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineStorer wraps ProfileStore and ConversationStore.
type BaselineStorer struct {
	profiles      *memory.ProfileStore
	conversations *memory.ConversationStore
}

// NewBaselineStorer creates a storer delegating to existing stores.
func NewBaselineStorer(profiles *memory.ProfileStore, conversations *memory.ConversationStore) *BaselineStorer {
	return &BaselineStorer{profiles: profiles, conversations: conversations}
}

// Store persists memory units. Profile units (Metadata["type"]=="profile") go to ProfileStore,
// conversation units go to ConversationStore.
func (s *BaselineStorer) Store(ctx context.Context, units []evolve.MemoryUnit) error {
	for _, u := range units {
		if u.Metadata["type"] == "profile" {
			continue // Profile updates handled by encoder via UpdateFromInteraction
		}
		contextID := u.Metadata["contextID"]
		if err := s.conversations.Append(u.Metadata["userId"], u.Role, u.Content, contextID); err != nil {
			return err
		}
	}
	return nil
}

// Load retrieves stored memory units for a user.
func (s *BaselineStorer) Load(ctx context.Context, userId string, filter evolve.StoreFilter) ([]evolve.MemoryUnit, error) {
	maxItems := filter.MaxItems
	if maxItems <= 0 {
		maxItems = 60
	}

	var units []evolve.MemoryUnit

	// Load profile as a system memory unit
	if filter.Type == "profile" || filter.Type == "all" || filter.Type == "" {
		profile, err := s.profiles.Load(userId)
		if err == nil && profile != nil {
			units = append(units, evolve.MemoryUnit{
				Role:    "system",
				Content: profile.Render(),
				Metadata: map[string]string{
					"type": "profile",
				},
			})
		}
	}

	// Load conversation entries
	if filter.Type == "conversation" || filter.Type == "all" || filter.Type == "" {
		var entries []memory.IndexEntry
		var err error

		if filter.ContextID != "" {
			entries, err = s.conversations.GetRecentIndexedByContext(userId, filter.ContextID, maxItems)
		} else {
			entries, err = s.conversations.GetRecentIndexed(userId, maxItems)
		}
		if err != nil {
			return units, err
		}

		for _, e := range entries {
			units = append(units, evolve.MemoryUnit{
				Role:     e.Role,
				Content:  e.Summary,
				Keywords: e.Keywords,
				Topics:   e.Topics,
				Metadata: map[string]string{
					"type":      "conversation",
					"contextID": e.ConversationContextID,
				},
			})
		}
	}

	return units, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/baseline/...`
Expected: Success. Note: this depends on `memory.IndexEntry` and `profile.Render()` being accessible. If `Render()` is unexported, we need to export it first — check `internal/memory/profile.go` and export `Render` if needed.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/baseline/storer.go
git commit -m "feat(evolve): add BaselineStorer wrapping ProfileStore + ConversationStore"
```

---

### Task 8: Create BaselineRetriever

**Files:**
- Create: `internal/memory/evolve/baseline/retriever.go`

- [ ] **Step 1: Write BaselineRetriever**

```go
// internal/memory/evolve/baseline/retriever.go
package baseline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineRetriever wraps the existing Jaccard + topic scoring logic.
type BaselineRetriever struct {
	profiles      *memory.ProfileStore
	conversations *memory.ConversationStore
	config        *evolve.RetrieverConfig
}

// NewBaselineRetriever creates a retriever using existing scoring.
func NewBaselineRetriever(profiles *memory.ProfileStore, conversations *memory.ConversationStore, config *evolve.RetrieverConfig) *BaselineRetriever {
	return &BaselineRetriever{
		profiles:      profiles,
		conversations: conversations,
		config:        config,
	}
}

// Retrieve selects the most relevant memory for a task, matching existing GetContextForUserWithTask behavior.
func (r *BaselineRetriever) Retrieve(ctx context.Context, query evolve.RetrievalQuery) (*evolve.RetrievalResult, error) {
	var sb strings.Builder

	// 1. Profile (stable prefix for prompt caching)
	profile, err := r.profiles.Load(query.UserID)
	if err == nil && profile != nil {
		sb.WriteString(profile.Render())
		sb.WriteString("\n\n")
	}

	// 2. Retrieve and score indexed entries
	maxResults := r.config.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	fallbackRecent := r.config.FallbackRecent
	if fallbackRecent <= 0 {
		fallbackRecent = 5
	}

	var entries []memory.IndexEntry
	if query.ContextID != "" {
		entries, err = r.conversations.GetRecentIndexedByContext(query.UserID, query.ContextID, 60)
	} else {
		entries, err = r.conversations.GetRecentIndexed(query.UserID, 60)
	}
	if err != nil {
		return &evolve.RetrievalResult{Context: sb.String(), TokenEstimate: len(sb.String()) / 4}, nil
	}

	if len(entries) == 0 {
		return &evolve.RetrievalResult{Context: sb.String(), TokenEstimate: len(sb.String()) / 4}, nil
	}

	taskKeywords := memory.ExtractKeywords(query.Task)
	taskTopics := memory.ExtractTopics(query.Task)

	type scored struct {
		entry memory.IndexEntry
		score float64
	}
	var scoredEntries []scored
	for _, e := range entries {
		s := scoreEntryBaseline(e, taskKeywords, taskTopics, r.config.TopicBonus)
		scoredEntries = append(scoredEntries, scored{entry: e, score: s})
	}

	sort.Slice(scoredEntries, func(i, j int) bool {
		return scoredEntries[i].score > scoredEntries[j].score
	})

	var selected []scored
	if scoredEntries[0].score > 0 {
		limit := maxResults
		if limit > len(scoredEntries) {
			limit = len(scoredEntries)
		}
		selected = scoredEntries[:limit]
	} else {
		limit := fallbackRecent
		if limit > len(scoredEntries) {
			limit = len(scoredEntries)
		}
		selected = scoredEntries[:limit]
	}

	// Apply "edges" positioning: highest scores at start and end
	if len(selected) > 2 {
		selected = positionEdges(selected)
	}

	sb.WriteString("## Relevant History\n")
	for _, s := range selected {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", s.entry.Role, s.entry.Summary))
	}

	text := sb.String()
	return &evolve.RetrievalResult{
		Context:       text,
		TokenEstimate: len(text) / 4,
		UnitsUsed:     len(selected),
	}, nil
}

// scoreEntryBaseline replicates the existing scoreEntry logic from index.go.
func scoreEntryBaseline(entry memory.IndexEntry, taskKeywords, taskTopics []string, topicBonus float64) float64 {
	if len(taskKeywords) == 0 {
		return 0
	}
	overlap := 0
	for _, tk := range taskKeywords {
		for _, ek := range entry.Keywords {
			if strings.EqualFold(tk, ek) {
				overlap++
				break
			}
		}
	}
	score := float64(overlap) / float64(len(taskKeywords))

	if topicBonus <= 0 {
		topicBonus = 0.2
	}
	for _, tt := range taskTopics {
		for _, et := range entry.Topics {
			if tt == et {
				score += topicBonus
				goto topicDone
			}
		}
	}
topicDone:
	return score
}

// positionEdges places highest-scored items at start and end (Lost in the Middle mitigation).
func positionEdges(items []scored) []scored {
	if len(items) <= 2 {
		return items
	}
	// Already sorted by score descending.
	// Put top half at edges: odd indices go to start, even to end.
	result := make([]scored, len(items))
	left, right := 0, len(items)-1
	for i, item := range items {
		if i%2 == 0 {
			result[left] = item
			left++
		} else {
			result[right] = item
			right--
		}
	}
	return result
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/baseline/...`
Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/baseline/retriever.go
git commit -m "feat(evolve): add BaselineRetriever with edges positioning"
```

---

### Task 9: Create BaselineManager

**Files:**
- Create: `internal/memory/evolve/baseline/manager.go`

- [ ] **Step 1: Write BaselineManager**

```go
// internal/memory/evolve/baseline/manager.go
package baseline

import (
	"context"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineManager wraps the existing Compactor.
type BaselineManager struct {
	compactor *memory.Compactor
}

// NewBaselineManager creates a manager delegating to existing compaction strategies.
func NewBaselineManager(compactor *memory.Compactor) *BaselineManager {
	return &BaselineManager{compactor: compactor}
}

// Compact delegates to the existing compactor.
func (m *BaselineManager) Compact(ctx context.Context, input evolve.CompactionInput) (*evolve.CompactionResult, error) {
	result := m.compactor.Compact(input.Text, input.MaxTokens)
	return &evolve.CompactionResult{
		Text:           result.Text,
		TokenEstimate:  result.TokenEstimate,
		EntriesRemoved: result.EntriesRemoved,
	}, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/baseline/...`
Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/baseline/manager.go
git commit -m "feat(evolve): add BaselineManager wrapping existing Compactor"
```

---

### Task 10: Write baseline integration tests

**Files:**
- Create: `internal/memory/evolve/baseline/baseline_test.go`

- [ ] **Step 1: Write baseline tests**

```go
// internal/memory/evolve/baseline/baseline_test.go
package baseline

import (
	"context"
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

	// Seed some conversation data
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

	// Write evolved config
	evolved := evolve.DefaultPipelineConfig()
	evolved.Version = 42
	evolved.Retriever.MaxResults = 20
	evolve.SavePipelineConfig(filepath.Join(evolvedDir, "pipeline-config.yaml"), evolved)

	// Get reference before reload
	before := holder.Get()

	// Reload
	if err := holder.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Old reference still valid (not nil)
	if before == nil {
		t.Fatal("old pipeline reference should still be valid")
	}

	// New reference has updated config
	after := holder.Get()
	if after.Config().Version != 42 {
		t.Fatalf("expected version 42, got %d", after.Config().Version)
	}
}
```

Note: `TestPipelineReloadAtomicSwap` requires additional imports: `"os"`, `"path/filepath"`, and `"github.com/MarcoMruz/agentloop/internal/memory/evolve"`. Add these to the test file imports.

- [ ] **Step 2: Run tests**

Run: `go test ./internal/memory/evolve/baseline/... -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/baseline/baseline_test.go
git commit -m "test(evolve): add baseline wrapper integration tests"
```

---

## Chunk 3: Metrics — Outcome Scoring, Clustering, Collection

### Task 11: Create TaskOutcome and scoring

**Files:**
- Create: `internal/memory/evolve/metrics/outcome.go`
- Create: `internal/memory/evolve/metrics/outcome_test.go`

- [ ] **Step 1: Write scoring tests**

```go
// internal/memory/evolve/metrics/outcome_test.go
package metrics

import (
	"testing"
	"time"
)

func TestScorePerfectTask(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done"}
	if s := o.Score(); s != 1.0 {
		t.Fatalf("expected 1.0, got %f", s)
	}
}

func TestScoreHITLDenials(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", HITLDenials: 2}
	expected := 0.5 // 1.0 - 2*0.25
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreSteers(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", SteerCount: 3}
	expected := 0.4 // 1.0 - 3*0.20
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreFloor(t *testing.T) {
	o := TaskOutcome{
		FinalStatus: "aborted",
		HITLDenials: 5,
		SteerCount:  5,
	}
	if s := o.Score(); s != 0.0 {
		t.Fatalf("expected 0.0, got %f", s)
	}
}

func TestScoreAbortedTask(t *testing.T) {
	o := TaskOutcome{FinalStatus: "aborted"}
	expected := 0.7 // 1.0 - 0.3
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreErrorTask(t *testing.T) {
	o := TaskOutcome{FinalStatus: "error"}
	expected := 0.8 // 1.0 - 0.2
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreHighTokenUsage(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", TokensUsed: 60000}
	expected := 0.9 // 1.0 - 0.1
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreHighToolCalls(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", ToolCalls: 40}
	expected := 0.9 // 1.0 - 0.1
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreCombined(t *testing.T) {
	o := TaskOutcome{
		FinalStatus: "done",
		HITLDenials: 1,
		SteerCount:  1,
		TokensUsed:  60000,
		ToolCalls:   40,
	}
	expected := 0.35 // 1.0 - 0.25 - 0.20 - 0.1 - 0.1
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestOutcomeJSONRoundTrip(t *testing.T) {
	o := TaskOutcome{
		SessionID:   "sess-abc",
		UserID:      "marco",
		Timestamp:   time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC),
		HITLDenials: 1,
		FinalStatus: "done",
	}
	data, err := o.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var o2 TaskOutcome
	if err := o2.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if o2.SessionID != o.SessionID {
		t.Fatalf("session mismatch: %s != %s", o2.SessionID, o.SessionID)
	}
	if o2.Score() != o.Score() {
		t.Fatalf("score mismatch: %f != %f", o2.Score(), o.Score())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/evolve/metrics/... -v`
Expected: FAIL — `TaskOutcome` not defined.

- [ ] **Step 3: Write TaskOutcome implementation**

```go
// internal/memory/evolve/metrics/outcome.go
package metrics

import (
	"encoding/json"
	"time"
)

// TaskOutcome records the result of a task session for evolution scoring.
type TaskOutcome struct {
	SessionID     string        `json:"session_id"`
	UserID        string        `json:"user_id"`
	Timestamp     time.Time     `json:"timestamp"`
	HITLDenials   int           `json:"hitl_denials"`
	HITLApprovals int           `json:"hitl_approvals"`
	SteerCount    int           `json:"steer_count"`
	FinalStatus   string        `json:"final_status"`
	TokensUsed    int           `json:"tokens_used"`
	ToolCalls     int           `json:"tool_calls"`
	Duration      time.Duration `json:"duration"`
	TaskKeywords  []string      `json:"task_keywords"`
	TaskTopics    []string      `json:"task_topics"`
	SkillsUsed    []string      `json:"skills_used"`
	PipelineID    string        `json:"pipeline_id"`
}

// Score computes a composite quality score (0.0 to 1.0).
func (o *TaskOutcome) Score() float64 {
	score := 1.0

	// Primary penalties
	score -= float64(o.HITLDenials) * 0.25
	score -= float64(o.SteerCount) * 0.20

	// Secondary penalties
	if o.FinalStatus == "aborted" {
		score -= 0.3
	}
	if o.FinalStatus == "error" {
		score -= 0.2
	}
	if o.TokensUsed > 50000 {
		score -= 0.1
	}
	if o.ToolCalls > 30 {
		score -= 0.1
	}

	if score < 0.0 {
		return 0.0
	}
	return score
}

// MarshalJSON serializes outcome to JSON.
func (o *TaskOutcome) MarshalJSON() ([]byte, error) {
	type Alias TaskOutcome
	return json.Marshal(&struct {
		*Alias
		Duration string `json:"duration"`
	}{
		Alias:    (*Alias)(o),
		Duration: o.Duration.String(),
	})
}

// UnmarshalJSON deserializes outcome from JSON.
func (o *TaskOutcome) UnmarshalJSON(data []byte) error {
	type Alias TaskOutcome
	aux := &struct {
		*Alias
		Duration string `json:"duration"`
	}{
		Alias: (*Alias)(o),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.Duration != "" {
		d, err := time.ParseDuration(aux.Duration)
		if err != nil {
			return err
		}
		o.Duration = d
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/evolve/metrics/... -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/evolve/metrics/outcome.go internal/memory/evolve/metrics/outcome_test.go
git commit -m "feat(evolve): add TaskOutcome with composite scoring"
```

---

### Task 12: Create topic-scoped outcome clustering

**Files:**
- Create: `internal/memory/evolve/metrics/cluster.go`
- Create: `internal/memory/evolve/metrics/cluster_test.go`

- [ ] **Step 1: Write clustering tests**

```go
// internal/memory/evolve/metrics/cluster_test.go
package metrics

import (
	"testing"
)

func TestClusterBySharedTopic(t *testing.T) {
	outcomes := []TaskOutcome{
		{SessionID: "a", TaskTopics: []string{"auth", "security"}},
		{SessionID: "b", TaskTopics: []string{"auth", "token"}},
		{SessionID: "c", TaskTopics: []string{"deploy", "docker"}},
	}
	clusters := ClusterOutcomes(outcomes, 10)
	// a and b share "auth", so they cluster together. c is separate.
	found := findClusterContaining(clusters, "a")
	if found == nil {
		t.Fatal("expected cluster containing a")
	}
	if !clusterContains(found, "b") {
		t.Fatal("a and b should be in same cluster (shared topic: auth)")
	}
	if clusterContains(found, "c") {
		t.Fatal("c should not be in same cluster as a")
	}
}

func TestClusterConnectedComponents(t *testing.T) {
	outcomes := []TaskOutcome{
		{SessionID: "a", TaskTopics: []string{"auth"}},
		{SessionID: "b", TaskTopics: []string{"auth", "database"}},
		{SessionID: "c", TaskTopics: []string{"database"}},
	}
	clusters := ClusterOutcomes(outcomes, 10)
	// a-b share auth, b-c share database → all connected
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster (connected components), got %d", len(clusters))
	}
	if len(clusters[0]) != 3 {
		t.Fatalf("expected 3 outcomes in cluster, got %d", len(clusters[0]))
	}
}

func TestClusterNoTopicsFallsBackToKeywords(t *testing.T) {
	outcomes := []TaskOutcome{
		{SessionID: "a", TaskKeywords: []string{"auth", "token", "refresh"}},
		{SessionID: "b", TaskKeywords: []string{"auth", "token", "expire"}},
		{SessionID: "c", TaskKeywords: []string{"deploy", "docker", "kubernetes"}},
	}
	clusters := ClusterOutcomes(outcomes, 10)
	found := findClusterContaining(clusters, "a")
	if found == nil {
		t.Fatal("expected cluster containing a")
	}
	if !clusterContains(found, "b") {
		t.Fatal("a and b should cluster (2+ shared keywords: auth, token)")
	}
	if clusterContains(found, "c") {
		t.Fatal("c should not cluster with a (0 shared keywords)")
	}
}

func TestClusterMaxSize(t *testing.T) {
	var outcomes []TaskOutcome
	for i := 0; i < 20; i++ {
		outcomes = append(outcomes, TaskOutcome{
			SessionID:  string(rune('a' + i)),
			TaskTopics: []string{"shared"},
		})
	}
	clusters := ClusterOutcomes(outcomes, 5)
	for _, c := range clusters {
		if len(c) > 5 {
			t.Fatalf("cluster exceeds max size 5, got %d", len(c))
		}
	}
}

func TestClusterEmpty(t *testing.T) {
	clusters := ClusterOutcomes(nil, 10)
	if len(clusters) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(clusters))
	}
}

func findClusterContaining(clusters [][]TaskOutcome, sessionID string) []TaskOutcome {
	for _, c := range clusters {
		for _, o := range c {
			if o.SessionID == sessionID {
				return c
			}
		}
	}
	return nil
}

func clusterContains(cluster []TaskOutcome, sessionID string) bool {
	for _, o := range cluster {
		if o.SessionID == sessionID {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/evolve/metrics/... -run TestCluster -v`
Expected: FAIL — `ClusterOutcomes` not defined.

- [ ] **Step 3: Write clustering implementation**

```go
// internal/memory/evolve/metrics/cluster.go
package metrics

// ClusterOutcomes groups outcomes by topic similarity using connected components.
// Two outcomes are connected if they share at least one topic. If no topics,
// falls back to keyword overlap (threshold: 2+ shared keywords).
// Each cluster is capped at maxSize.
func ClusterOutcomes(outcomes []TaskOutcome, maxSize int) [][]TaskOutcome {
	if len(outcomes) == 0 {
		return nil
	}

	n := len(outcomes)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Connect outcomes that share topics or keywords
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if connected(outcomes[i], outcomes[j]) {
				union(i, j)
			}
		}
	}

	// Group by root
	groups := make(map[int][]int)
	for i := 0; i < n; i++ {
		r := find(i)
		groups[r] = append(groups[r], i)
	}

	// Build clusters, splitting if over maxSize
	var clusters [][]TaskOutcome
	for _, indices := range groups {
		var cluster []TaskOutcome
		for _, idx := range indices {
			cluster = append(cluster, outcomes[idx])
			if len(cluster) >= maxSize {
				clusters = append(clusters, cluster)
				cluster = nil
			}
		}
		if len(cluster) > 0 {
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

// connected returns true if two outcomes share a topic or 2+ keywords.
func connected(a, b TaskOutcome) bool {
	// Check topics first
	if len(a.TaskTopics) > 0 && len(b.TaskTopics) > 0 {
		for _, at := range a.TaskTopics {
			for _, bt := range b.TaskTopics {
				if at == bt {
					return true
				}
			}
		}
	}

	// Fall back to keywords if either has no topics
	if len(a.TaskTopics) == 0 || len(b.TaskTopics) == 0 {
		shared := 0
		for _, ak := range a.TaskKeywords {
			for _, bk := range b.TaskKeywords {
				if ak == bk {
					shared++
					if shared >= 2 {
						return true
					}
					break
				}
			}
		}
	}

	return false
}

// FindClusterFor returns the cluster containing the given outcome.
func FindClusterFor(clusters [][]TaskOutcome, sessionID string) []TaskOutcome {
	for _, c := range clusters {
		for _, o := range c {
			if o.SessionID == sessionID {
				return c
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/evolve/metrics/... -run TestCluster -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/evolve/metrics/cluster.go internal/memory/evolve/metrics/cluster_test.go
git commit -m "feat(evolve): add topic-scoped outcome clustering"
```

---

### Task 13: Create Collector with JSONL persistence and rate limiting

**Files:**
- Create: `internal/memory/evolve/metrics/collector.go`
- Create: `internal/memory/evolve/metrics/collector_test.go`

- [ ] **Step 1: Write collector tests**

```go
// internal/memory/evolve/metrics/collector_test.go
package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectorPersist(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 300, 10)

	outcome := TaskOutcome{
		SessionID:   "sess-123",
		UserID:      "marco",
		Timestamp:   time.Now(),
		FinalStatus: "done",
	}

	c.Record(outcome)

	// Check JSONL file was created
	dateStr := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, "metrics", "marco-"+dateStr+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected metrics file: %v", err)
	}
	if !strings.Contains(string(data), "sess-123") {
		t.Fatal("expected session ID in metrics file")
	}
}

func TestCollectorRateLimitingCooldown(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 60, 10) // 60s cooldown

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	bad := TaskOutcome{
		SessionID:   "sess-1",
		UserID:      "marco",
		Timestamp:   time.Now(),
		FinalStatus: "aborted",
		HITLDenials: 3,
	}

	c.Record(bad)
	if triggered != 1 {
		t.Fatalf("expected 1 trigger, got %d", triggered)
	}

	// Second bad outcome immediately — should be rate-limited
	bad.SessionID = "sess-2"
	c.Record(bad)
	if triggered != 1 {
		t.Fatalf("expected still 1 trigger (cooldown), got %d", triggered)
	}
}

func TestCollectorRateLimitingDailyCap(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 0, 2) // 0s cooldown, max 2/day

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	bad := TaskOutcome{
		FinalStatus: "aborted",
		HITLDenials: 3,
		Timestamp:   time.Now(),
	}

	for i := 0; i < 5; i++ {
		bad.SessionID = string(rune('a' + i))
		c.Record(bad)
	}

	if triggered != 2 {
		t.Fatalf("expected 2 triggers (daily cap), got %d", triggered)
	}
}

func TestCollectorGoodScoreNoTrigger(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, 0.7, 0, 100)

	triggered := 0
	c.SetEvolutionTrigger(func(o TaskOutcome) {
		triggered++
	})

	good := TaskOutcome{
		SessionID:   "sess-ok",
		FinalStatus: "done",
		Timestamp:   time.Now(),
	}
	c.Record(good)

	if triggered != 0 {
		t.Fatalf("expected 0 triggers for good score, got %d", triggered)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/evolve/metrics/... -run TestCollector -v`
Expected: FAIL — `NewCollector` not defined.

- [ ] **Step 3: Write Collector implementation**

```go
// internal/memory/evolve/metrics/collector.go
package metrics

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EvolutionTriggerFunc is called when a poor outcome triggers evolution.
type EvolutionTriggerFunc func(outcome TaskOutcome)

// Collector persists task outcomes to JSONL and triggers evolution on poor scores.
type Collector struct {
	mu                 sync.Mutex
	vaultPath          string
	scoreThreshold     float64
	minCooldownSeconds int
	maxDailyRuns       int
	lastTriggerTime    time.Time
	dailyTriggerCount  int
	dailyTriggerDate   string
	trigger            EvolutionTriggerFunc
}

// NewCollector creates a collector with rate limiting parameters.
func NewCollector(vaultPath string, scoreThreshold float64, minCooldownSeconds, maxDailyRuns int) *Collector {
	metricsDir := filepath.Join(vaultPath, "metrics")
	os.MkdirAll(metricsDir, 0755)

	return &Collector{
		vaultPath:          vaultPath,
		scoreThreshold:     scoreThreshold,
		minCooldownSeconds: minCooldownSeconds,
		maxDailyRuns:       maxDailyRuns,
	}
}

// SetEvolutionTrigger sets the function called when evolution should run.
func (c *Collector) SetEvolutionTrigger(fn EvolutionTriggerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trigger = fn
}

// Record persists an outcome and triggers evolution if score is below threshold.
func (c *Collector) Record(outcome TaskOutcome) {
	c.persist(outcome)

	score := outcome.Score()
	if score >= c.scoreThreshold {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.trigger == nil {
		return
	}

	now := time.Now()

	// Rate limit: cooldown
	if c.minCooldownSeconds > 0 {
		elapsed := now.Sub(c.lastTriggerTime)
		if elapsed < time.Duration(c.minCooldownSeconds)*time.Second {
			slog.Debug("evolution rate-limited (cooldown)", "elapsed", elapsed)
			return
		}
	}

	// Rate limit: daily cap
	today := now.Format("2006-01-02")
	if c.dailyTriggerDate != today {
		c.dailyTriggerDate = today
		c.dailyTriggerCount = 0
	}
	if c.dailyTriggerCount >= c.maxDailyRuns {
		slog.Debug("evolution rate-limited (daily cap)", "count", c.dailyTriggerCount)
		return
	}

	c.lastTriggerTime = now
	c.dailyTriggerCount++

	trigger := c.trigger
	go trigger(outcome)
}

// persist writes an outcome to the JSONL metrics file.
func (c *Collector) persist(outcome TaskOutcome) {
	if outcome.Timestamp.IsZero() {
		outcome.Timestamp = time.Now()
	}
	dateStr := outcome.Timestamp.Format("2006-01-02")
	dir := filepath.Join(c.vaultPath, "metrics")
	os.MkdirAll(dir, 0755)

	path := filepath.Join(dir, outcome.UserID+"-"+dateStr+".jsonl")
	data, err := json.Marshal(outcome)
	if err != nil {
		slog.Error("failed to marshal outcome", "error", err)
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("failed to open metrics file", "error", err)
		return
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
}

// LoadOutcomes reads all outcomes for a user from JSONL files.
func LoadOutcomes(vaultPath, userId string, maxDays int) ([]TaskOutcome, error) {
	dir := filepath.Join(vaultPath, "metrics")
	var outcomes []TaskOutcome

	for d := 0; d < maxDays; d++ {
		date := time.Now().AddDate(0, 0, -d).Format("2006-01-02")
		path := filepath.Join(dir, userId+"-"+date+".jsonl")

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		lines := splitLines(data)
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			var o TaskOutcome
			if err := json.Unmarshal(line, &o); err != nil {
				continue
			}
			outcomes = append(outcomes, o)
		}
	}

	return outcomes, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/evolve/metrics/... -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/evolve/metrics/collector.go internal/memory/evolve/metrics/collector_test.go
git commit -m "feat(evolve): add metrics Collector with JSONL persistence and rate limiting"
```

---

## Chunk 4: Meta-Agent — Proposals, Prompt, Applier

### Task 14: Create EvolutionProposal types

**Files:**
- Create: `internal/memory/evolve/meta/proposal.go`

- [ ] **Step 1: Write proposal types**

```go
// internal/memory/evolve/meta/proposal.go
package meta

import (
	"encoding/json"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// EvolutionProposal is the structured output from the meta-agent.
type EvolutionProposal struct {
	Reasoning     string               `json:"reasoning"`
	ConfigChanges *evolve.PipelineConfig `json:"config_changes"`
	SkillChanges  []SkillProposal      `json:"skill_changes"`
	AgentsMDPatch string               `json:"agents_md_patch"`
	Summary       string               `json:"summary"`
}

// SkillProposal describes a change to a skill file.
type SkillProposal struct {
	Action      string   `json:"action"` // "create", "update", "delete"
	Name        string   `json:"name"`
	Triggers    []string `json:"triggers"`
	Description string   `json:"description"`
	Content     string   `json:"content"` // Full SKILL.md body
}

// SkillSummary is a lightweight view of an existing skill for the prompt.
type SkillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Triggers    []string `json:"triggers"`
}

// ParseProposal extracts an EvolutionProposal from the meta-agent's text output.
// It looks for a JSON block in the response.
func ParseProposal(text string) (*EvolutionProposal, error) {
	// Find JSON block — look for first { and last }
	start := -1
	end := -1
	depth := 0
	for i, c := range text {
		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}
	if start == -1 || end == -1 {
		return nil, &json.SyntaxError{}
	}

	var proposal EvolutionProposal
	if err := json.Unmarshal([]byte(text[start:end]), &proposal); err != nil {
		return nil, err
	}
	return &proposal, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/meta/...`
Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/meta/proposal.go
git commit -m "feat(evolve): add EvolutionProposal and SkillProposal types"
```

---

### Task 15: Create evolution prompt builder

**Files:**
- Create: `internal/memory/evolve/meta/prompt.go`

- [ ] **Step 1: Write prompt builder**

```go
// internal/memory/evolve/meta/prompt.go
package meta

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
)

// EvolutionPrompt holds all context for the meta-agent.
type EvolutionPrompt struct {
	SystemContext string
	Outcomes      []metrics.TaskOutcome
	CurrentConfig *evolve.PipelineConfig
	CurrentSkills []SkillSummary
	AgentsMD      string
	Constraints   []string
}

// BuildEvolutionPrompt assembles the full prompt text for the meta-agent.
func BuildEvolutionPrompt(p EvolutionPrompt) string {
	var sb strings.Builder

	sb.WriteString(p.SystemContext)
	sb.WriteString("\n\n")

	// Task outcomes
	sb.WriteString("## Recent Poor Task Outcomes\n\n")
	for i, o := range p.Outcomes {
		sb.WriteString(fmt.Sprintf("### Outcome %d (score: %.2f)\n", i+1, o.Score()))
		sb.WriteString(fmt.Sprintf("- Session: %s\n", o.SessionID))
		sb.WriteString(fmt.Sprintf("- Status: %s\n", o.FinalStatus))
		sb.WriteString(fmt.Sprintf("- HITL Denials: %d\n", o.HITLDenials))
		sb.WriteString(fmt.Sprintf("- Steers: %d\n", o.SteerCount))
		sb.WriteString(fmt.Sprintf("- Tokens: %d\n", o.TokensUsed))
		sb.WriteString(fmt.Sprintf("- Tool Calls: %d\n", o.ToolCalls))
		if len(o.TaskTopics) > 0 {
			sb.WriteString(fmt.Sprintf("- Topics: %s\n", strings.Join(o.TaskTopics, ", ")))
		}
		if len(o.TaskKeywords) > 0 {
			sb.WriteString(fmt.Sprintf("- Keywords: %s\n", strings.Join(o.TaskKeywords, ", ")))
		}
		sb.WriteString("\n")
	}

	// Current config
	sb.WriteString("## Current Pipeline Configuration\n\n```json\n")
	cfgJSON, _ := json.MarshalIndent(p.CurrentConfig, "", "  ")
	sb.Write(cfgJSON)
	sb.WriteString("\n```\n\n")

	// Current skills
	if len(p.CurrentSkills) > 0 {
		sb.WriteString("## Active Skills\n\n")
		for _, s := range p.CurrentSkills {
			sb.WriteString(fmt.Sprintf("- **%s**: %s (triggers: %s)\n", s.Name, s.Description, strings.Join(s.Triggers, ", ")))
		}
		sb.WriteString("\n")
	}

	// Current AGENTS.md evolved section
	if p.AgentsMD != "" {
		sb.WriteString("## Current AGENTS.md Evolved Section\n\n")
		sb.WriteString(p.AgentsMD)
		sb.WriteString("\n\n")
	}

	// Constraints
	sb.WriteString("## Constraints\n\n")
	for _, c := range p.Constraints {
		sb.WriteString(fmt.Sprintf("- %s\n", c))
	}
	sb.WriteString("\n")

	// Response format
	sb.WriteString("## Required Response Format\n\n")
	sb.WriteString("Respond with a single JSON object:\n\n```json\n")
	sb.WriteString(`{
  "reasoning": "Why these changes will help",
  "config_changes": { ... pipeline config fields to change ... },
  "skill_changes": [
    {"action": "create|update|delete", "name": "evolved-NAME", "triggers": [...], "description": "...", "content": "..."}
  ],
  "agents_md_patch": "New content for the EVOLVED section (or empty)",
  "summary": "One-line summary for git commit"
}`)
	sb.WriteString("\n```\n")

	return sb.String()
}

// DefaultSystemContext returns the system prompt for the meta-agent.
func DefaultSystemContext() string {
	return `You are MemEvolve, a meta-evolution agent for the AgentLoop system.

Your job: analyze poor task outcomes and propose improvements to make future tasks succeed.

You can propose three types of changes:
1. **Pipeline config changes** — adjust retrieval parameters, compaction strategy, keyword limits, topic extensions
2. **Skill changes** — create or update skills (behavioral patterns) that will be loaded into future agent prompts
3. **AGENTS.md changes** — add learned patterns to the agent's core instructions

Guidelines:
- Focus on the specific topic cluster of failures. Don't make broad changes for narrow problems.
- Skills should have precise triggers so they only activate for relevant tasks.
- AGENTS.md changes should be concise bullet points, not lengthy instructions.
- Config changes should be conservative — small parameter tweaks, not radical rewrites.
- All skill names MUST be prefixed with "evolved-".`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/meta/...`
Expected: Success.

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/meta/prompt.go
git commit -m "feat(evolve): add evolution prompt builder"
```

---

### Task 16: Create Applier with AGENTS.md marker protection

**Files:**
- Create: `internal/memory/evolve/meta/applier.go`
- Create: `internal/memory/evolve/meta/applier_test.go`

- [ ] **Step 1: Write applier tests**

```go
// internal/memory/evolve/meta/applier_test.go
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

	original := `# Agent Instructions

Some important rules here.

<!-- EVOLVED:START -->
## Learned Patterns

- Old pattern
<!-- EVOLVED:END -->

More content after.
`
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

	// Valid: evolved- prefix
	err := a.ApplySkill(SkillProposal{
		Action:  "create",
		Name:    "evolved-auth-patterns",
		Content: "# Auth Patterns\n\nSome instructions.",
		Triggers: []string{"auth", "token"},
		Description: "Auth handling improvements",
	})
	if err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}

	// Invalid: no evolved- prefix
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

	// gitCommit should not panic when git is unavailable (temp dir is not a repo)
	// This is a graceful degradation test — just verify no crash
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/evolve/meta/... -run "TestApplier|TestProposal" -v`
Expected: FAIL — `NewApplier` not defined.

- [ ] **Step 3: Write Applier implementation**

```go
// internal/memory/evolve/meta/applier.go
package meta

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/version"
)

const (
	evolvedStartMarker = "<!-- EVOLVED:START -->"
	evolvedEndMarker   = "<!-- EVOLVED:END -->"
)

// Applier writes evolution proposals to disk.
type Applier struct {
	vaultPath  string
	agentsMD   string
	skillsPath string
}

// NewApplier creates an applier for the given vault.
func NewApplier(vaultPath, agentsMDPath, skillsPath string) *Applier {
	return &Applier{
		vaultPath:  vaultPath,
		agentsMD:   agentsMDPath,
		skillsPath: skillsPath,
	}
}

// Apply executes a full evolution proposal.
func (a *Applier) Apply(proposal *EvolutionProposal) error {
	// 1. Snapshot
	_, err := a.Snapshot()
	if err != nil {
		slog.Warn("snapshot failed, continuing", "error", err)
	}

	// 2. Config
	if proposal.ConfigChanges != nil {
		if err := a.ApplyConfig(proposal.ConfigChanges); err != nil {
			return fmt.Errorf("apply config: %w", err)
		}
	}

	// 3. Skills
	for _, sp := range proposal.SkillChanges {
		if err := a.ApplySkill(sp); err != nil {
			slog.Warn("skill apply failed", "name", sp.Name, "error", err)
		}
	}

	// 4. AGENTS.md
	if proposal.AgentsMDPatch != "" && a.agentsMD != "" {
		if err := a.ApplyAgentsMD(proposal.AgentsMDPatch); err != nil {
			slog.Warn("agents.md apply failed", "error", err)
		}
	}

	// 5. Git commit
	a.gitCommit(proposal.Summary)

	// 6. Log
	a.logEvolution(proposal)

	return nil
}

// ApplyConfig writes the pipeline config to the evolved directory.
func (a *Applier) ApplyConfig(cfg *evolve.PipelineConfig) error {
	dir := filepath.Join(a.vaultPath, "memory", "evolved")
	os.MkdirAll(dir, 0755)
	return evolve.SavePipelineConfig(filepath.Join(dir, "pipeline-config.yaml"), cfg)
}

// ApplySkill creates, updates, or deletes an evolved skill.
func (a *Applier) ApplySkill(sp SkillProposal) error {
	if !strings.HasPrefix(sp.Name, "evolved-") {
		return fmt.Errorf("skill name must be prefixed with 'evolved-', got: %s", sp.Name)
	}

	skillDir := filepath.Join(a.skillsPath, sp.Name)

	switch sp.Action {
	case "delete":
		return os.RemoveAll(skillDir)
	case "create", "update":
		os.MkdirAll(skillDir, 0755)
		content := buildSkillMD(sp)
		return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
	default:
		return fmt.Errorf("unknown skill action: %s", sp.Action)
	}
}

// ApplyAgentsMD replaces content between EVOLVED markers in AGENTS.md.
func (a *Applier) ApplyAgentsMD(patch string) error {
	data, err := os.ReadFile(a.agentsMD)
	if err != nil {
		return err
	}

	content := string(data)
	evolvedBlock := fmt.Sprintf("%s\n%s\n%s", evolvedStartMarker, patch, evolvedEndMarker)

	startIdx := strings.Index(content, evolvedStartMarker)
	endIdx := strings.Index(content, evolvedEndMarker)

	if startIdx == -1 || endIdx == -1 {
		// No markers — append at end
		content = content + "\n" + evolvedBlock + "\n"
	} else {
		// Replace between markers (inclusive)
		content = content[:startIdx] + evolvedBlock + content[endIdx+len(evolvedEndMarker):]
	}

	return os.WriteFile(a.agentsMD, []byte(content), 0644)
}

// Snapshot delegates to the version.Snapshotter to avoid duplication.
func (a *Applier) Snapshot() (string, error) {
	snap := version.NewSnapshotter(a.vaultPath, a.agentsMD)
	return snap.Take()
}

func (a *Applier) gitCommit(summary string) {
	vaultDir := a.vaultPath

	// Lazy git init
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = vaultDir
	if err := cmd.Run(); err != nil {
		// Not a git repo — initialize
		init := exec.Command("git", "init")
		init.Dir = vaultDir
		if err := init.Run(); err != nil {
			slog.Warn("git init failed", "error", err)
			return
		}
		// Create .gitignore
		gitignore := "memory/evolved/metrics/*.jsonl\nmemory/cache/\n"
		os.WriteFile(filepath.Join(vaultDir, ".gitignore"), []byte(gitignore), 0644)
	}

	// Stage and commit
	add := exec.Command("git", "add", "-A")
	add.Dir = vaultDir
	if err := add.Run(); err != nil {
		slog.Warn("git add failed", "error", err)
		return
	}

	msg := "evolve: " + summary
	commit := exec.Command("git", "commit", "-m", msg, "--allow-empty")
	commit.Dir = vaultDir
	if err := commit.Run(); err != nil {
		slog.Debug("git commit skipped", "error", err)
	}
}

func (a *Applier) logEvolution(proposal *EvolutionProposal) {
	logPath := filepath.Join(a.vaultPath, "memory", "evolved", "evolution-log.jsonl")
	entry := fmt.Sprintf(`{"timestamp":"%s","summary":"%s","reasoning":"%s"}`,
		time.Now().Format(time.RFC3339),
		strings.ReplaceAll(proposal.Summary, `"`, `\"`),
		strings.ReplaceAll(proposal.Reasoning, `"`, `\"`),
	)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(entry + "\n")
}

func buildSkillMD(sp SkillProposal) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", sp.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", sp.Description))
	sb.WriteString("triggers:\n")
	for _, t := range sp.Triggers {
		sb.WriteString(fmt.Sprintf("  - %s\n", t))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(sp.Content)
	return sb.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/evolve/meta/... -run TestApplier -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/evolve/meta/applier.go internal/memory/evolve/meta/applier_test.go
git commit -m "feat(evolve): add Applier with AGENTS.md marker protection and skill namespacing"
```

---

### Task 17: Create version snapshot and log

**Files:**
- Create: `internal/memory/evolve/version/snapshot.go`
- Create: `internal/memory/evolve/version/log.go`
- Create: `internal/memory/evolve/version/version_test.go`

- [ ] **Step 1: Write version tests**

```go
// internal/memory/evolve/version/version_test.go
package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvolutionLogAppend(t *testing.T) {
	dir := t.TempDir()
	log := NewEvolutionLog(dir)

	log.Append(LogEntry{Summary: "first change", ConfigVersion: 1})
	log.Append(LogEntry{Summary: "second change", ConfigVersion: 2})

	data, err := os.ReadFile(filepath.Join(dir, "evolution-log.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "first change") {
		t.Fatal("first entry missing")
	}
	if !strings.Contains(lines[1], "second change") {
		t.Fatal("second entry missing")
	}
}

func TestSnapshotContainsAllFiles(t *testing.T) {
	dir := t.TempDir()
	evolvedDir := filepath.Join(dir, "memory", "evolved")
	os.MkdirAll(evolvedDir, 0755)
	os.WriteFile(filepath.Join(evolvedDir, "pipeline-config.yaml"), []byte("version: 1"), 0644)

	agentsMD := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(agentsMD, []byte("<!-- EVOLVED:START -->\ntest\n<!-- EVOLVED:END -->"), 0644)

	snap := NewSnapshotter(dir, agentsMD)
	ts, err := snap.Take()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	snapshotDir := filepath.Join(evolvedDir, "snapshots", ts)

	// Check config was copied
	if _, err := os.Stat(filepath.Join(snapshotDir, "pipeline-config.yaml")); err != nil {
		t.Fatal("pipeline-config.yaml not in snapshot")
	}

	// Check agents-md section was extracted
	if _, err := os.Stat(filepath.Join(snapshotDir, "agents-md-section.md")); err != nil {
		t.Fatal("agents-md-section.md not in snapshot")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/memory/evolve/version/... -v`
Expected: FAIL — types not defined.

- [ ] **Step 3: Write version implementations**

```go
// internal/memory/evolve/version/log.go
package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LogEntry represents one evolution event.
type LogEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	SessionID     string    `json:"session_id"`
	Score         float64   `json:"score"`
	Summary       string    `json:"summary"`
	ConfigVersion int       `json:"config_version"`
}

// EvolutionLog manages the append-only evolution log.
type EvolutionLog struct {
	path string
}

// NewEvolutionLog creates a log writer.
func NewEvolutionLog(evolvedDir string) *EvolutionLog {
	return &EvolutionLog{path: filepath.Join(evolvedDir, "evolution-log.jsonl")}
}

// Append adds an entry to the log.
func (l *EvolutionLog) Append(entry LogEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
	return nil
}
```

```go
// internal/memory/evolve/version/snapshot.go
package version

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Snapshotter creates point-in-time copies of evolved state.
type Snapshotter struct {
	vaultPath string
	agentsMD  string
}

// NewSnapshotter creates a snapshotter.
func NewSnapshotter(vaultPath, agentsMDPath string) *Snapshotter {
	return &Snapshotter{vaultPath: vaultPath, agentsMD: agentsMDPath}
}

// Take creates a snapshot and returns the timestamp identifier.
func (s *Snapshotter) Take() (string, error) {
	ts := time.Now().Format("20060102-150405")
	evolvedDir := filepath.Join(s.vaultPath, "memory", "evolved")
	snapshotDir := filepath.Join(evolvedDir, "snapshots", ts)

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", err
	}

	// Copy pipeline config
	src := filepath.Join(evolvedDir, "pipeline-config.yaml")
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(filepath.Join(snapshotDir, "pipeline-config.yaml"), data, 0644)
	}

	// Extract and copy AGENTS.md evolved section
	if s.agentsMD != "" {
		if data, err := os.ReadFile(s.agentsMD); err == nil {
			content := string(data)
			start := strings.Index(content, "<!-- EVOLVED:START -->")
			end := strings.Index(content, "<!-- EVOLVED:END -->")
			if start != -1 && end != -1 {
				section := content[start : end+len("<!-- EVOLVED:END -->")]
				os.WriteFile(filepath.Join(snapshotDir, "agents-md-section.md"), []byte(section), 0644)
			}
		}
	}

	return ts, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/memory/evolve/version/... -v`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/evolve/version/
git commit -m "feat(evolve): add version snapshot and evolution log"
```

---

## Chunk 5: Meta-Agent Core and Session Integration

### Task 18: Create MetaAgent with pi session lifecycle

**Files:**
- Create: `internal/memory/evolve/meta/agent.go`

- [ ] **Step 1: Write MetaAgent**

```go
// internal/memory/evolve/meta/agent.go
package meta

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MarcoMruz/agentloop/internal/bridge"
	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
)
```

Note: `bridge.RPCEvent` has fields `Type string`, `AssistantMessageEvent *AssistantMessageEvent` (with `.Type` and `.Text`). The handler matches the `EventHandler func(event RPCEvent) error` signature from `bridge/rpc.go:20`. `bridge.New()` takes 3 args: `piCfg`, `secCfg`, `hitlCfg`. `b.Prompt()` takes `ctx`, `id`, `text`.

Remove the duplicate closing of imports below (the original had the closing paren already):
```go
// FIXME: verify this compiles — the bridge API is the most integration-sensitive part
)

// MetaAgent manages the evolution lifecycle: collect context, run pi, apply changes.
type MetaAgent struct {
	mu          sync.Mutex
	vaultPath   string
	agentsMDPath string
	skillsPath  string
	piCfg       config.PiConfig
	secCfg      config.SecurityConfig
	evoCfg      config.EvolutionConfig
	pipeline    *evolve.PipelineHolder
}

// NewMetaAgent creates a meta-agent.
func NewMetaAgent(
	vaultPath, agentsMDPath, skillsPath string,
	piCfg config.PiConfig,
	secCfg config.SecurityConfig,
	evoCfg config.EvolutionConfig,
	pipeline *evolve.PipelineHolder,
) *MetaAgent {
	return &MetaAgent{
		vaultPath:    vaultPath,
		agentsMDPath: agentsMDPath,
		skillsPath:   skillsPath,
		piCfg:        piCfg,
		secCfg:       secCfg,
		evoCfg:       evoCfg,
		pipeline:     pipeline,
	}
}

// Evolve runs the evolution loop for a poor-scoring outcome.
func (m *MetaAgent) Evolve(outcome metrics.TaskOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("evolution starting", "session", outcome.SessionID, "score", outcome.Score())

	// 1. Collect topic-scoped context
	allOutcomes, err := metrics.LoadOutcomes(
		filepath.Join(m.vaultPath, "memory", "evolved"),
		outcome.UserID,
		30,
	)
	if err != nil {
		slog.Error("failed to load outcomes", "error", err)
		return
	}

	clusters := metrics.ClusterOutcomes(allOutcomes, m.evoCfg.MaxOutcomesPerRun)
	cluster := metrics.FindClusterFor(clusters, outcome.SessionID)
	if cluster == nil {
		cluster = []metrics.TaskOutcome{outcome}
	}

	// 2. Build prompt
	currentConfig := evolve.DefaultPipelineConfig()
	if m.pipeline != nil {
		if p := m.pipeline.Get(); p != nil {
			currentConfig = p.Config()
		}
	}

	agentsMDEvolved := m.readEvolvedSection()

	prompt := BuildEvolutionPrompt(EvolutionPrompt{
		SystemContext: DefaultSystemContext(),
		Outcomes:      cluster,
		CurrentConfig: currentConfig,
		AgentsMD:      agentsMDEvolved,
		Constraints: []string{
			"All skill names must be prefixed with 'evolved-'",
			"AGENTS.md changes are limited to the EVOLVED section",
			"Config changes should be conservative — small tweaks, not rewrites",
			"Maintain vault compatibility (Markdown/YAML format)",
			"Position must remain 'edges' for retriever (Lost in the Middle)",
		},
	})

	// 3. Run pi session
	response, err := m.runPiSession(prompt)
	if err != nil {
		slog.Error("pi session failed", "error", err)
		return
	}

	// 4. Parse proposal
	proposal, err := ParseProposal(response)
	if err != nil {
		slog.Error("failed to parse evolution proposal", "error", err)
		return
	}

	// 5. Apply
	applier := NewApplier(m.vaultPath, m.agentsMDPath, m.skillsPath)
	if err := applier.Apply(proposal); err != nil {
		slog.Error("failed to apply evolution", "error", err)
		return
	}

	// 6. Hot-swap
	if m.pipeline != nil {
		if err := m.pipeline.Reload(); err != nil {
			slog.Error("pipeline reload failed", "error", err)
		}
	}

	slog.Info("evolution complete", "summary", proposal.Summary)
}

// runPiSession launches a dedicated read-only pi subprocess and returns its text response.
func (m *MetaAgent) runPiSession(prompt string) (string, error) {
	ctx := context.Background()

	// Create temporary work directory with copies of relevant data
	workDir, err := os.MkdirTemp("", "memevolve-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	b := bridge.New(m.piCfg, m.secCfg, config.HITLConfig{})

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

	if err := b.Prompt(ctx, "meta-evolve", prompt); err != nil {
		return "", fmt.Errorf("prompt pi: %w", err)
	}

	<-b.Done()
	return response.String(), nil
}

func (m *MetaAgent) readEvolvedSection() string {
	if m.agentsMDPath == "" {
		return ""
	}
	data, err := os.ReadFile(m.agentsMDPath)
	if err != nil {
		return ""
	}
	content := string(data)
	start := strings.Index(content, "<!-- EVOLVED:START -->")
	end := strings.Index(content, "<!-- EVOLVED:END -->")
	if start == -1 || end == -1 {
		return ""
	}
	return content[start+len("<!-- EVOLVED:START -->") : end]
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/memory/evolve/meta/...`
Expected: Success (or compilation errors if bridge API doesn't match — adjust `SetEventHandler` and `Prompt` calls to match actual bridge API from `internal/bridge/rpc.go`).

- [ ] **Step 3: Commit**

```bash
git add internal/memory/evolve/meta/agent.go
git commit -m "feat(evolve): add MetaAgent with pi session lifecycle"
```

---

### Task 19: Add session counters for HITL denials and steers

**Files:**
- Modify: `internal/session/session.go:33-53` (Session struct)
- Modify: `internal/session/session.go:137-150` (ResolveHITL)
- Modify: `internal/session/session.go:105-112` (Steer)

- [ ] **Step 1: Add atomic counter fields to Session struct**

In `internal/session/session.go`, add to the Session struct (around line 33-53):
```go
hitlDenials int32
steerCount  int32
```

- [ ] **Step 2: Add counter increment in ResolveHITL**

In `ResolveHITL()` (around line 137-150), add after the decision is processed:
```go
if decision == "deny" {
	atomic.AddInt32(&s.hitlDenials, 1)
}
```

Add `"sync/atomic"` to imports if not present.

- [ ] **Step 3: Add counter increment in Steer**

In `Steer()` (around line 105-112), add at the start of the method:
```go
atomic.AddInt32(&s.steerCount, 1)
```

- [ ] **Step 4: Add accessor methods**

Add after the existing methods:
```go
// HITLDenialCount returns the number of HITL denials in this session.
func (s *Session) HITLDenialCount() int {
	return int(atomic.LoadInt32(&s.hitlDenials))
}

// SteerCount returns the number of steer commands in this session.
func (s *Session) SteerCount() int {
	return int(atomic.LoadInt32(&s.steerCount))
}
```

- [ ] **Step 5: Run existing tests**

Run: `go test ./internal/session/... -v`
Expected: All existing tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go
git commit -m "feat(session): add HITL denial and steer counters"
```

---

### Task 20: Wire metrics collector into session manager

**Files:**
- Modify: `internal/session/manager.go:161-174`

- [ ] **Step 1: Add collector field to Manager struct**

In `internal/session/manager.go`, add to Manager struct (around line 43-54):
```go
collector *metrics.Collector
```

Add import: `"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"`

- [ ] **Step 2: Add setter method**

```go
// SetMetricsCollector sets the evolution metrics collector.
func (m *Manager) SetMetricsCollector(c *metrics.Collector) {
	m.collector = c
}
```

- [ ] **Step 3: Add collector call after task completion**

In `StartSession()` goroutine, after `m.memory.RecordInteraction()` (around line 174), add:
```go
if m.collector != nil {
	m.collector.Record(metrics.TaskOutcome{
		SessionID:    sess.ID,
		UserID:       sess.UserID,
		Timestamp:    time.Now(),
		HITLDenials:  sess.HITLDenialCount(),
		SteerCount:   sess.SteerCount(),
		FinalStatus:  string(sess.State),
		TaskKeywords: memory.ExtractKeywords(sess.Task),
		TaskTopics:   memory.ExtractTopics(sess.Task),
	})
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: Success.

- [ ] **Step 5: Commit**

```bash
git add internal/session/manager.go
git commit -m "feat(session): wire metrics collector into task completion"
```

---

### Task 21: Wire Pipeline into Memory Engine (delegation model)

**Files:**
- Modify: `internal/memory/engine.go:10-16`

- [ ] **Step 1: Add pipeline field to Engine**

In `internal/memory/engine.go`, add to Engine struct:
```go
pipeline *evolve.PipelineHolder
```

Add import: `"github.com/MarcoMruz/agentloop/internal/memory/evolve"`

- [ ] **Step 2: Add setter method**

```go
// SetPipeline sets the MemEvolve pipeline for delegation.
func (e *Engine) SetPipeline(p *evolve.PipelineHolder) {
	e.pipeline = p
}
```

- [ ] **Step 3: Add delegation in GetContextForUserWithTask**

At the start of `GetContextForUserWithTask()` (line 111), add:
```go
if e.pipeline != nil {
	p := e.pipeline.Get()
	if p != nil {
		result, err := p.Retrieve(context.Background(), evolve.RetrievalQuery{
			UserID:    userId,
			Task:      task,
			MaxTokens: e.maxCtxTokens,
		})
		if err == nil {
			return result.Context, nil
		}
		slog.Warn("pipeline retrieve failed, falling back", "error", err)
	}
}
```

Add `"context"` to imports if not present.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: All tests pass. The pipeline is nil by default, so existing behavior is preserved.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/engine.go
git commit -m "feat(memory): add Pipeline delegation in Engine"
```

---

## Chunk 6: Final Assembly and Full Test Suite

### Task 22: Run full test suite and fix any issues

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass.

- [ ] **Step 2: Run security tests specifically**

Run: `go test ./internal/security/... -v && go test ./internal/bridge/... -v`
Expected: All security tests pass.

- [ ] **Step 3: Fix any compilation or test failures**

If any tests fail, fix the issues. Common issues:
- Import path mismatches (use `github.com/MarcoMruz/agentloop/internal/...`)
- Unexported methods being called from baseline wrappers (export them)
- Type mismatches between existing code and new interfaces

- [ ] **Step 4: Commit any fixes**

```bash
git add internal/memory/evolve/ internal/session/ internal/config/ internal/memory/engine.go internal/memory/index.go configs/agentloop.yaml
git commit -m "fix: resolve compilation and test issues"
```

---

### Task 23: Verify end-to-end compilation

- [ ] **Step 1: Build both binaries**

Run: `go build -o /dev/null ./cmd/agentloop-server && go build -o /dev/null ./cmd/agentloop`
Expected: Both binaries compile successfully.

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues.

- [ ] **Step 3: Final commit if needed**

```bash
git add internal/memory/evolve/ internal/session/ internal/config/ internal/memory/engine.go internal/memory/index.go configs/agentloop.yaml
git commit -m "chore: final cleanup after MemEvolve implementation"
```
