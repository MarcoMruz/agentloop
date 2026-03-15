# MemEvolve: Self-Evolving Memory Architecture for AgentLoop

**Date:** 2026-03-15
**Status:** Draft
**Approach:** Parallel Tracks (Interface Abstraction + Meta-Agent Infrastructure)

---

## 1. Problem Statement

AgentLoop's memory engine is monolithic and static. Four tightly-coupled components (ProfileStore, ConversationStore, Compactor, PromptCache) use hardcoded heuristics — Jaccard scoring, stem matching against a fixed 31-topic taxonomy, three rigid compaction strategies. Nothing adapts based on task outcomes. The same retrieval and compaction logic applies whether the task succeeds brilliantly or fails repeatedly.

**Goal:** Transform the memory engine into a self-evolving system that learns from task outcomes, proposes improvements via a dedicated meta-evolution LLM agent, and autonomously applies those improvements so future tasks benefit.

---

## 2. Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| LLM access for meta-agent | Dedicated pi session | Consistent with existing architecture; no new LLM client |
| Strategy representation | Declarative YAML config + skills + AGENTS.md | Safe, inspectable, git-trackable |
| Evolution scope | Memory config (auto) + Skills (auto) + AGENTS.md (auto, scoped markers) | Full autonomy with visibility via git history |
| Success signals | Primary: HITL denials (0.25), steers (0.20). Secondary: status, tokens, tool calls | Strongest human-feedback signals weighted highest |
| Evolution trigger | Async background, only on poor outcomes (score < 0.7) | No wasted LLM calls on successful tasks |
| Outcome scoping | Topic-clustered, not user-global | Prevents incoherent evolution from mixing unrelated task failures |
| Versioning | Vault snapshots (runtime) + git commits (audit) | Dual-track: fast loading + full history |
| Human approval | Fully autonomous | User reviews via git log and vault inspection |
| Meta-agent security | Read-only pi session; Go applier writes all files | Pi cannot modify AGENTS.md or skills directly; structured JSON proposal only |
| Concurrency | Serialized evolution runs via mutex | Prevents race conditions on config/skills/git |
| Rate limiting | Cooldown + daily cap | Prevents runaway LLM costs from pathological failure patterns |

---

## 3. The Four MemEvolve Interfaces

These Go interfaces abstract the four pillars of memory behavior. Each has a baseline implementation wrapping existing code.

### 3.1 Interface Definitions

```go
// internal/memory/evolve/interfaces.go

type Encoder interface {
    // Encode transforms a raw interaction into structured memory units.
    Encode(ctx context.Context, input EncoderInput) (*EncoderOutput, error)
}

type Storer interface {
    // Store persists encoded memory units to the vault.
    Store(ctx context.Context, units []MemoryUnit) error
    // Load retrieves stored units for a user, optionally filtered.
    Load(ctx context.Context, userId string, filter StoreFilter) ([]MemoryUnit, error)
}

type Retriever interface {
    // Retrieve selects the most relevant memory for a given task context.
    Retrieve(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error)
}

type Manager interface {
    // Compact consolidates/prunes stored memory under a token budget.
    Compact(ctx context.Context, input CompactionInput) (*CompactionResult, error)
}
```

### 3.2 Universal Exchange Type

```go
type MemoryUnit struct {
    ID        string
    Timestamp time.Time
    Role      string            // "user", "assistant", "system"
    Content   string
    Keywords  []string
    Topics    []string
    Metadata  map[string]string // Extensible: contextID, source, toolsUsed, etc.
    Score     float64           // Set by Retriever during scoring
}
```

### 3.3 Pipeline Orchestrator

The existing Engine becomes a thin wrapper delegating to a Pipeline:

```go
type Pipeline struct {
    encoder   Encoder
    storer    Storer
    retriever Retriever
    manager   Manager
    config    *PipelineConfig
}
```

Baseline implementations (`BaselineEncoder`, `BaselineStorer`, `BaselineRetriever`, `BaselineManager`) wrap existing code 1:1. Zero behavior change on day one.

**Storer impedance mismatch:** The baseline wraps two stores with different data models — `ProfileStore` (YAML, one file per user) and `ConversationStore` (Markdown + JSON index, per-day files). `BaselineStorer` handles this by treating the profile as a special `MemoryUnit` with `Role: "system"` and `Metadata["type"] = "profile"`. The `Store()` method dispatches based on this metadata field. The `Load()` method's `StoreFilter` includes a `Type` field to request profile units, conversation units, or both.

---

## 4. Task Outcome Scoring & Metrics

### 4.1 TaskOutcome Structure

```go
type TaskOutcome struct {
    SessionID    string
    UserID       string
    Timestamp    time.Time

    // Primary signals (highest weight)
    HITLDenials  int
    HITLApprovals int
    SteerCount   int

    // Secondary signals
    FinalStatus  string        // "done", "aborted", "error"
    TokensUsed   int
    ToolCalls    int
    Duration     time.Duration

    // Context
    TaskKeywords []string
    TaskTopics   []string
    SkillsUsed   []string
    PipelineID   string        // Active evolved pipeline config version
}
```

### 4.2 Composite Score

```go
func (o *TaskOutcome) Score() float64 {
    score := 1.0

    // Primary penalties
    score -= float64(o.HITLDenials) * 0.25
    score -= float64(o.SteerCount) * 0.20

    // Secondary penalties
    if o.FinalStatus == "aborted" { score -= 0.3 }
    if o.FinalStatus == "error"   { score -= 0.2 }
    if o.TokensUsed > 50000       { score -= 0.1 }
    if o.ToolCalls > 30           { score -= 0.1 }

    if score < 0.0 {
        return 0.0
    }
    return score
}
```

Note: Avoids `max()` builtin for Go <1.21 compatibility (see CLAUDE.md Gotcha #2 regarding local `min()` definitions).

### 4.3 Storage

Outcomes appended to `vault/memory/evolved/metrics/{userId}-YYYY-MM-DD.jsonl` — one JSON line per task.

### 4.4 Evolution Trigger

After scoring, if `score < 0.7`, the meta-evolution agent launches asynchronously in a background goroutine, subject to rate limits (see Section 5.7).

### 4.5 Metrics Retention

Metrics JSONL files are retained for `ConversationRetainDays` (default 30 days), matching the conversation store retention policy. The collector prunes expired files on startup.

### 4.6 Populating Context Fields

`TaskKeywords` and `TaskTopics` are extracted from the original task text using exported versions of `extractKeywords()` and `extractTopics()` from `index.go` (currently unexported — must be exported as `ExtractKeywords()` and `ExtractTopics()`). `SkillsUsed` comes from the PromptBuilder's skill detection. `PipelineID` is read from the active `PipelineConfig.Version`.

---

## 5. Meta-Evolution Agent

### 5.1 Overview

A dedicated pi session that analyzes poor task outcomes and proposes improvements to the memory pipeline, skills, and agent instructions.

### 5.2 Lifecycle

```
TaskOutcome.Score() < 0.7
    │
    ▼
MetaAgent.Evolve(outcome)
    │
    ├─ 1. Collect context (topic-scoped):
    │     a. Cluster recent outcomes by topic similarity
    │        (reuse keyword/topic overlap scoring from index.go)
    │     b. Select only outcomes from same topic cluster as trigger
    │     c. Load: cluster-relevant outcomes (up to 10),
    │        current pipeline config, relevant skills, AGENTS.md
    │
    ├─ 2. Acquire evolution mutex (only one Evolve() runs at a time)
    │     - If already running, queue outcome for next run
    │
    ├─ 3. Spin up dedicated pi session via PiBridge
    │     - workDir: temporary read-only directory with copies of metrics/config
    │     - No HITL handler (fully autonomous)
    │     - Token budget: 10k cap
    │     - Pi tools restricted: read-only (no write/edit/bash)
    │     - Pi returns structured JSON proposal only
    │
    ├─ 4. Send evolution prompt (structured):
    │     EvolutionPrompt with outcomes + config + skills + constraints
    │
    ├─ 5. Parse response as EvolutionProposal (structured JSON)
    │     - If parsing fails, log error and abort (no partial writes)
    │
    ├─ 6. Go applier writes all changes (pi never writes directly):
    │     ├─ Memory config → vault/memory/evolved/pipeline-config.yaml
    │     ├─ Skills → vault/skills/evolved-{name}/SKILL.md
    │     └─ AGENTS.md → replace between <!-- EVOLVED:START/END --> markers
    │
    ├─ 7. Snapshot current state to vault/memory/evolved/snapshots/{timestamp}/
    │
    ├─ 8. Git commit: "evolve: {summary}"
    │
    ├─ 9. Hot-swap: pipeline.Reload()
    │
    └─ 10. Release evolution mutex
```

### 5.3 Topic-Scoped Context Collection

Outcomes are clustered by topic similarity before being fed to the meta-agent. This prevents incoherent evolution from mixing unrelated task failures (e.g., auth bugs + k8s deploys + invoice templates).

**Clustering algorithm:** Two outcomes are in the same cluster if they share at least one topic. Grouping uses connected components — if outcome A shares a topic with B, and B shares a topic with C, all three are in one cluster. When an outcome has no topics, it falls back to keyword overlap (threshold: at least 2 shared keywords). Maximum cluster size is capped at `MaxOutcomesPerRun` (default 10). This reuses the exported `ExtractKeywords()` and `ExtractTopics()` functions from `index.go`.

### 5.4 Evolution Prompt

```go
type EvolutionPrompt struct {
    SystemContext string
    Outcomes      []TaskOutcome
    CurrentConfig *PipelineConfig
    CurrentSkills []SkillSummary
    AgentsMD      string
    Constraints   []string
}
```

### 5.5 Evolution Proposal

```go
type EvolutionProposal struct {
    Reasoning     string
    ConfigChanges *PipelineConfig
    SkillChanges  []SkillProposal
    AgentsMDPatch string
    Summary       string
}

type SkillProposal struct {
    Action      string // "create", "update", "delete"
    Name        string
    Triggers    []string
    Description string
    Content     string
}
```

### 5.6 Constraints Enforced in Code

- **Read-only pi session:** The meta-agent's pi subprocess is restricted to read-only tools. It cannot write files, run bash commands, or edit files. It returns a structured JSON proposal that the Go applier validates and writes.
- **AGENTS.md marker protection:** The Go applier enforces that only content between `<!-- EVOLVED:START -->` and `<!-- EVOLVED:END -->` markers is modified. If markers are missing, the applier creates them at the end of the file.
- **Skills namespacing:** Evolved skills go under `vault/skills/evolved-{name}/`. The applier rejects any skill name not prefixed with `evolved-`.
- **Token budget:** Pi session capped at 10k tokens.
- **No HITL handler:** Meta-agent sessions run fully autonomous.
- **Serialized execution:** Only one evolution run at a time (mutex-protected).

### 5.7 Rate Limiting

To prevent runaway LLM costs from pathological failure patterns:

```go
type RateLimiter struct {
    MinCooldownSeconds int // Minimum seconds between evolution runs (default: 300)
    MaxDailyRuns       int // Maximum evolution runs per day (default: 10)
}
```

Added to `EvolutionConfig`. If cooldown hasn't elapsed or daily cap is reached, the outcome is logged to metrics but no evolution run is triggered. The rate limiter state is tracked in memory (reset on server restart — conservative, since restart resets the daily counter to 0).

---

## 6. Pipeline Configuration & Hot-Swapping

### 6.1 Configuration File

```yaml
# vault/memory/evolved/pipeline-config.yaml
version: 3
updated: 2026-03-15T14:00:00Z
evolved_from: "sess-a1b2c3d4"

encoder:
  strategy: "baseline"
  keyword_limit: 15
  topic_taxonomy_extensions:
    - "auth-refresh"
    - "k8s-networking"
  extract_tool_patterns: true

retriever:
  strategy: "jaccard"
  max_results: 8
  fallback_recent: 5
  topic_bonus: 0.2
  recency_weight: 0.0
  position: "edges"            # Lost in the Middle mitigation — hard constraint

manager:
  strategy: "rolling"
  rolling_keep_recent: 5
  facts_max: 30
  facts_keywords:
    - "decided"
    - "confirmed"
    - "created"
    - "fixed"
    - "deployed"
    - "merged"
    - "installed"
    - "configured"
    - "updated"
    - "deleted"
    - "error"
    - "failed"
  aggressive_filter: false

storer:
  format: "markdown"
  index_sidecar: true
  context_isolation: true
```

### 6.2 Runtime Loading

```go
func NewPipeline(vaultPath string, defaults *PipelineConfig) *Pipeline {
    evolved := loadEvolvedConfig(vaultPath)
    config := mergeWithDefaults(defaults, evolved)

    return &Pipeline{
        encoder:   newEncoder(config.Encoder),
        storer:    newStorer(config.Storer),
        retriever: newRetriever(config.Retriever),
        manager:   newManager(config.Manager),
        config:    config,
    }
}
```

### 6.3 Hot-Swap

Pipeline exposes `Reload()`. After the meta-agent writes new config, it calls `Reload()`.

**Concurrency model:** `Reload()` creates a new Pipeline instance with updated implementations, then atomically swaps a pointer (`atomic.Pointer[Pipeline]`). Active sessions that already hold a reference to the old Pipeline continue using it until their task completes. New sessions acquire the current pointer and get the evolved Pipeline. No mutex contention on the read path.

### 6.4 Lost in the Middle Enforcement

The `position: "edges"` retriever setting is a hard constraint. Highest-scoring memory units are placed at the top and bottom of the `<memory>` block, lower-scoring units in the middle. Not overridable by evolution.

### 6.5 Distractor Interference Mitigation

The `aggressive_filter` manager setting enables aggressive pruning of irrelevant history during compaction, rather than summarizing it. The meta-agent can toggle this per topic cluster.

---

## 7. Change Application & Versioning

### 7.1 Apply Flow

```
EvolutionProposal received
    │
    ├─ 1. Snapshot current state
    │     └─ vault/memory/evolved/snapshots/{timestamp}/
    │        ├─ pipeline-config.yaml
    │        ├─ skills-manifest.json
    │        └─ agents-md-section.md
    │
    ├─ 2. Apply config changes
    │     └─ Write vault/memory/evolved/pipeline-config.yaml (version++)
    │
    ├─ 3. Apply skill changes
    │     ├─ Create: vault/skills/evolved-{name}/SKILL.md
    │     ├─ Update: overwrite existing SKILL.md
    │     └─ Delete: remove directory
    │
    ├─ 4. Apply AGENTS.md changes (if any)
    │     └─ Replace between <!-- EVOLVED:START --> and <!-- EVOLVED:END -->
    │
    ├─ 5. Git commit: "evolve: {proposal.Summary}"
    │
    ├─ 6. Hot-swap: pipeline.Reload()
    │
    └─ 7. Log to vault/memory/evolved/evolution-log.jsonl
```

### 7.2 AGENTS.md Protection

```markdown
# AgentLoop Agent Instructions

... (manually written content — never touched by evolution) ...

<!-- EVOLVED:START -->
## Learned Patterns

- When handling auth token tasks, check for token expiry before refresh attempts
- Prefer git stash over branch switching for quick fixes
<!-- EVOLVED:END -->
```

The meta-agent can only write between the markers. Everything outside is read-only.

### 7.3 Git Initialization

The vault directory (`~/.local/share/agentloop/vault/`) is not a git repository by default. The applier lazily initializes git on the first evolution run:

1. Check if `vault/` is inside a git repo (`git rev-parse --git-dir`)
2. If not, run `git init` in the vault directory
3. Create `.gitignore` excluding large/transient files: `memory/evolved/metrics/*.jsonl`, `memory/cache/`
4. If git is not installed, log a warning and skip git operations (snapshots still work for rollback)

The git repo scope is the entire vault directory, so session files, memory profiles, and evolved config are all tracked.

### 7.4 Rollback

Forward-only by design (fully autonomous). Self-healing: the meta-agent sees worsening scores and proposes corrections. Manual recovery available via:
- `git log --oneline -- vault/memory/evolved/`
- `vault/memory/evolved/snapshots/{timestamp}/`
- `vault/memory/evolved/evolution-log.jsonl`

---

## 8. Package Structure

```
internal/memory/
├── engine.go              # Existing — thin wrapper delegating to Pipeline
├── profile.go             # Existing — unchanged, used by BaselineStorer
├── conversation.go        # Existing — unchanged, used by BaselineStorer
├── compaction.go          # Existing — unchanged, used by BaselineManager
├── cache.go               # Existing — unchanged
├── index.go               # Existing — unchanged, used by BaselineRetriever
│
└── evolve/
    ├── interfaces.go      # Encoder, Storer, Retriever, Manager + MemoryUnit
    ├── pipeline.go        # Pipeline orchestrator
    ├── config.go          # PipelineConfig, YAML loading, mergeWithDefaults
    │
    ├── baseline/
    │   ├── encoder.go     # Wraps existing heuristic extraction
    │   ├── storer.go      # Wraps ProfileStore + ConversationStore
    │   ├── retriever.go   # Wraps Jaccard + topic scoring
    │   └── manager.go     # Wraps three compaction strategies
    │
    ├── metrics/
    │   ├── outcome.go     # TaskOutcome + Score()
    │   ├── collector.go   # Collects signals, writes JSONL, triggers meta-agent
    │   └── cluster.go     # Topic-scoped outcome clustering
    │
    ├── meta/
    │   ├── agent.go       # MetaAgent — pi session lifecycle
    │   ├── prompt.go      # EvolutionPrompt construction
    │   ├── proposal.go    # EvolutionProposal + SkillProposal structs
    │   └── applier.go     # Applies proposals: config, skills, AGENTS.md, git, snapshot
    │
    └── version/
        ├── snapshot.go    # Snapshot to timestamped directory
        └── log.go         # Evolution log (JSONL)
```

---

## 9. Integration Points

### 9.1 Changes to Existing Code

| File | Change | Risk |
|------|--------|------|
| `session/session.go` | Add `hitlDenials int32` and `steerCount int32` fields with `atomic.AddInt32` increments. `ResolveHITL()` increments denial counter when `decision == "deny"`. `Steer()` increments steer counter. New methods: `HITLDenialCount() int` and `SteerCount() int`. | Low — additive, atomic counters are lock-free |
| `session/manager.go` | Call metrics collector after task completion | Low — additive |
| `agent/core.go` | No change — continues passing Engine to PromptBuilder | None |
| `agent/prompt_builder.go` | No change — continues calling `Engine.GetContext*()` | None |
| `memory/engine.go` | Add Pipeline field. Engine's public methods (`GetContextForUserWithTask`, `GetContextForUserAndConversationContext`, `RecordInteraction`) delegate internally to Pipeline when available, falling back to existing code when Pipeline is nil. This is the **delegation model** — PromptBuilder and agent core are unaware of the Pipeline. | Low — backward compatible, single integration point |
| `memory/index.go` | Export `ExtractKeywords()` and `ExtractTopics()` (currently unexported) | Low — additive |
| `config/config.go` | Add `EvolutionConfig` section | Low — additive |

### 9.2 Session → Metrics

```go
// session/manager.go — after task completion
// RunResult is available after agent.Core.Run() completes and sess.SetResult() is called.
// Session needs a new Stats() accessor that returns RunResult.Stats.
result := sess.Result()
collector.Record(TaskOutcome{
    SessionID:   sess.ID,
    UserID:      sess.UserID,
    HITLDenials: sess.HITLDenialCount(),
    SteerCount:  sess.SteerCount(),
    FinalStatus: string(sess.Status()),
    TokensUsed:  result.Stats.Tokens,
    ToolCalls:   result.Stats.ToolCalls,
    TaskKeywords: evolve.ExtractKeywords(sess.Task()),
    TaskTopics:   evolve.ExtractTopics(sess.Task()),
    SkillsUsed:  result.SkillsUsed,
    PipelineID:  pipeline.ConfigVersion(),
})
```

### 9.3 Metrics → Meta-Agent

```go
// evolve/metrics/collector.go
func (c *Collector) Record(outcome TaskOutcome) {
    c.persist(outcome)
    if outcome.Score() < 0.7 {
        go c.metaAgent.Evolve(outcome)
    }
}
```

### 9.4 Meta-Agent → PiBridge

Reuses existing PiBridge. No HITL handler. Token budget capped. workDir set to `vault/memory/evolved/`.

### 9.5 Pipeline → Skills Registry

Evolved skills in `vault/skills/evolved-{name}/` are picked up by the existing Registry on next `DetectSkills()` call. No registry changes needed.

---

## 10. Configuration

### 10.1 Top-Level Config Integration

Added to the main `Config` struct in `config.go`:

```go
Evolution EvolutionConfig `mapstructure:"evolution"`
```

Path fields (`PipelineConfigPath`) get `expandHome()` treatment in `Load()`.

### 10.2 Config Struct

```go
type EvolutionConfig struct {
    Enabled           bool    `mapstructure:"enabled"`
    ScoreThreshold    float64 `mapstructure:"score_threshold"`
    MetaTokenBudget   int     `mapstructure:"meta_token_budget"`
    MaxOutcomesPerRun int     `mapstructure:"max_outcomes_per_run"`
    SnapshotRetainMax  int    `mapstructure:"snapshot_retain_max"`
    PipelineConfigPath string `mapstructure:"pipeline_config_path"`
    MinCooldownSeconds int    `mapstructure:"min_cooldown_seconds"`
    MaxDailyRuns       int    `mapstructure:"max_daily_runs"`
}
```

### 10.3 Defaults

```go
Evolution: EvolutionConfig{
    Enabled:           true,
    ScoreThreshold:    0.7,
    MetaTokenBudget:   10000,
    MaxOutcomesPerRun: 10,
    SnapshotRetainMax:  50,
    PipelineConfigPath: "", // Auto-resolves to vault/memory/evolved/pipeline-config.yaml
    MinCooldownSeconds: 300,
    MaxDailyRuns:       10,
}
```

### 10.4 YAML

```yaml
evolution:
  enabled: true
  score_threshold: 0.7
  meta_token_budget: 10000
  max_outcomes_per_run: 10
  snapshot_retain_max: 50
  min_cooldown_seconds: 300
  max_daily_runs: 10
```

---

## 11. Vault Structure (Extended)

```
~/.local/share/agentloop/vault/
├── sessions/                          # Existing — unchanged
├── memory/
│   ├── users/                         # Existing — unchanged
│   ├── contexts/                      # Existing — unchanged
│   ├── cache/                         # Existing — reserved
│   └── evolved/                       # NEW
│       ├── pipeline-config.yaml       # Active evolved configuration
│       ├── evolution-log.jsonl        # Append-only evolution audit log
│       ├── metrics/
│       │   └── {userId}-YYYY-MM-DD.jsonl  # Task outcome metrics
│       └── snapshots/
│           └── {timestamp}/           # Point-in-time recovery
│               ├── pipeline-config.yaml
│               ├── skills-manifest.json
│               └── agents-md-section.md
└── skills/
    ├── (manually created skills)      # Existing — unchanged
    └── evolved-{name}/                # NEW — meta-agent created
        └── SKILL.md
```

---

## 12. Constraints & Invariants

1. **Vault compatibility** — All storage remains Markdown/YAML, browsable in Obsidian.
2. **Lost in the Middle** — `position: "edges"` is a hard constraint on all retriever implementations. Critical facts always at prompt start/end.
3. **Distractor interference** — Manager must filter irrelevant history aggressively, not just summarize.
4. **No new dependencies** — Everything uses stdlib + existing deps (uuid, yaml, viper).
5. **Thread isolation preserved** — ConversationContextID scoping unchanged.
6. **AGENTS.md protection** — Only `<!-- EVOLVED:START/END -->` section is writable by meta-agent.
7. **Skill namespacing** — Evolved skills prefixed with `evolved-` to distinguish from manual ones.
8. **Forward-only evolution** — No automated rollback; self-healing via continued evolution.
9. **Provider agnostic** — Meta-agent uses pi subprocess, works with any configured LLM provider.
10. **Meta-agent is read-only** — Pi session cannot write files; Go applier is the sole writer.
11. **Serialized evolution** — Only one evolution run at a time via mutex.
12. **Rate limited** — Minimum cooldown between runs + daily cap.
13. **Score-only positive signals missing** — Scoring only has negative signals (penalties). A task where the agent produces poor output but encounters no HITL gates scores 1.0. This is a known limitation; future iterations may add positive signals (e.g., user explicit feedback).

---

## 13. Test Plan

Tests follow existing conventions: `*_test.go` in same package, `TestFunctionNameBehavior` naming.

| Package | Test | Verifies |
|---------|------|----------|
| `evolve/metrics` | `TestScorePerfectTask` | Score = 1.0 when no penalties |
| `evolve/metrics` | `TestScoreHITLDenials` | Each denial reduces score by 0.25 |
| `evolve/metrics` | `TestScoreSteers` | Each steer reduces score by 0.20 |
| `evolve/metrics` | `TestScoreFloor` | Score never goes below 0.0 |
| `evolve/metrics` | `TestScoreAbortedTask` | Aborted status applies 0.3 penalty |
| `evolve/metrics` | `TestClusterBySharedTopic` | Outcomes with shared topics cluster together |
| `evolve/metrics` | `TestClusterConnectedComponents` | Transitive topic sharing groups correctly |
| `evolve/metrics` | `TestClusterNoTopicsFallsBackToKeywords` | Keyword overlap used when no topics |
| `evolve/metrics` | `TestCollectorRateLimiting` | Cooldown and daily cap enforced |
| `evolve` | `TestPipelineConfigLoad` | YAML deserialization of pipeline-config.yaml |
| `evolve` | `TestPipelineConfigMergeDefaults` | Evolved config merges over defaults correctly |
| `evolve` | `TestPipelineReloadAtomicSwap` | Concurrent readers don't see partial state |
| `evolve/baseline` | `TestBaselineEncoderWrapsExisting` | Same output as existing `UpdateFromInteraction` |
| `evolve/baseline` | `TestBaselineRetrieverWrapsExisting` | Same output as existing `GetContextForUserWithTask` |
| `evolve/meta` | `TestApplierAgentsMDMarkerProtection` | Only content between markers is modified |
| `evolve/meta` | `TestApplierAgentsMDCreatesMarkers` | Markers created if missing |
| `evolve/meta` | `TestApplierSkillNamespacing` | Rejects skills not prefixed with `evolved-` |
| `evolve/meta` | `TestApplierSnapshotCreated` | Snapshot directory created before changes |
| `evolve/meta` | `TestApplierGitNotInstalled` | Graceful degradation when git unavailable |
| `evolve/meta` | `TestProposalParsingInvalid` | Invalid JSON proposal logged and aborted |
| `evolve/version` | `TestSnapshotContainsAllFiles` | Snapshot includes config, skills manifest, agents-md section |
| `evolve/version` | `TestEvolutionLogAppend` | JSONL append works correctly |
