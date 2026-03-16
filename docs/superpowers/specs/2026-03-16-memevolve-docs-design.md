# MemEvolve Documentation — Design Spec

**Date:** 2026-03-16
**Status:** Draft
**Scope:** New `memevolve/` documentation section for the agentloop-docs Astro/Starlight site, plus cross-links from existing sections.

---

## 1. Goals

Produce comprehensive, reference-grade documentation for MemEvolve — AgentLoop's self-evolving memory pipeline — that serves two audiences on the same pages:

- **Operators** running AgentLoop who need to understand what evolution does, how to tune it, and how to recover from a bad evolution.
- **Developers** building on or extending AgentLoop who need Go type signatures, data flow, algorithmic detail, and a working custom-implementation tutorial.

Success criteria:
- A developer unfamiliar with MemEvolve can build a custom `Encoder` from the tutorial alone.
- An operator can understand the scoring formula, tune thresholds, and roll back an evolution without reading source code.
- Every key Go type and method is documented inline with actual signatures (not pseudocode).

---

## 2. Site Placement

**New section:** `src/content/docs/memevolve/` (7 pages)

**Cross-link additions** (no content changes, only `:::tip See also` callouts):
- `src/content/docs/architecture/memory-system.mdx` — link to `memevolve/pipeline`
- `src/content/docs/reference/configuration-reference.mdx` — link to `memevolve/configuration`

**Sidebar nav entry** in `astro.config.mjs`:
```js
{
  label: 'MemEvolve',
  items: [
    'memevolve/overview',
    'memevolve/how-memory-works',
    'memevolve/scoring-and-triggers',
    'memevolve/evolution-loop',
    'memevolve/versioning-and-safety',
    'memevolve/configuration',
    'memevolve/custom-encoder',
  ],
}
```

---

## 3. Page Design

Each page follows this template:

```
---
title: <Page Title>
description: <One-line SEO description>
---

## Overview          ← operator-facing: 2–4 sentences, what this means for you
## [Conceptual section(s)] ← named per page topic, diagrams + data-flow prose
## [Developer deep-dive(s)] ← Go type signatures, key method bodies, annotated
```

Callout conventions (Starlight):
- `:::note` — important nuances
- `:::caution` — operator warnings (e.g., rate limits, cost implications)
- `:::tip` — cross-links and "see also" references
- `:::danger` — safety constraints that must not be violated

Code blocks: Go for type signatures and method bodies; YAML for config examples; JSON for wire formats.

---

## 4. Pages

### 4.1 `overview.mdx` — What Is MemEvolve?

**Purpose:** Entry point. Sets the mental model before any technical detail.

**Operator summary:** MemEvolve watches how tasks go and quietly improves the memory system over time. When a session ends badly (too many course-corrections, tool overuse, errors), it proposes targeted changes — updated retrieval config, new skills, or new instructions for the agent — and applies them automatically. You can inspect every change via `evolution-log.jsonl` or `git log`.

**Sections:**
1. **The problem it solves** — Why static memory degrades; the "wrong context for the task" failure mode.
2. **How it works (one paragraph + flow diagram)** — Four-stage loop: Observe → Score → Cluster → Evolve. ASCII diagram showing the feedback loop from session completion back into the pipeline.
3. **What it can change** — Bullet list: pipeline retrieval config, evolved skills (`evolved-*`), the `<!-- EVOLVED -->` section of AGENTS.md.
4. **What it cannot change** — Hard constraints: content outside AGENTS.md markers, hand-authored skills (non `evolved-` prefix), security policies, core server config.
5. **Key guarantees** — Numbered list: serialized (one evolution at a time), rate-limited, snapshot-before-apply, git-tracked, no downtime (atomic hot-swap).

---

### 4.2 `how-memory-works.mdx` — The Memory Pipeline

**Purpose:** Explains the four interfaces, `MemoryUnit`, `Pipeline`/`PipelineHolder`, and the baseline implementations. This is the "data flows through here" page.

**Operator summary:** AgentLoop builds the memory block injected into every prompt by running your interactions through a four-stage pipeline: encode → store → retrieve → compact. Each stage can be swapped independently. The baseline implementations match the original behavior exactly.

**Sections:**
1. **The four interfaces** — Each interface defined with its Go signature and a one-sentence purpose:
   ```go
   type Encoder interface {
       Encode(ctx context.Context, input EncoderInput) (*EncoderOutput, error)
   }
   type Storer interface {
       Store(ctx context.Context, units []MemoryUnit) error
       Load(ctx context.Context, userId string, filter StoreFilter) ([]MemoryUnit, error)
   }
   type Retriever interface {
       Retrieve(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error)
   }
   type Manager interface {
       Compact(ctx context.Context, input CompactionInput) (*CompactionResult, error)
   }
   ```

2. **`MemoryUnit` — the universal exchange type** — Full struct with field-by-field annotations:
   ```go
   type MemoryUnit struct {
       ID        string
       Timestamp time.Time
       Role      string            // "user" | "assistant" | "system"
       Content   string
       Keywords  []string          // extracted by Encoder
       Topics    []string          // matched against taxonomy
       Metadata  map[string]string // arbitrary key-value pairs
       Score     float64           // set by Retriever during scoring
   }
   ```

3. **`Pipeline` and `PipelineHolder`** — How the four interfaces are wired together; the `atomic.Pointer[Pipeline]` hot-swap model. Diagram showing in-flight sessions keeping old pipeline reference while new sessions get the reloaded one:
   ```
   Old sessions ──► Pipeline v2 (until they finish)
   New sessions  ──► Pipeline v3 (after Reload())
   ```

4. **Baseline implementations** — One subsection per:
   - `BaselineEncoder`: calls `ExtractKeywords`/`ExtractTopics` from `memory/index.go`, produces one `MemoryUnit` per role
   - `BaselineStorer`: delegates to `ProfileStore` + `ConversationStore`
   - `BaselineRetriever`: Jaccard keyword scoring + topic bonus + recency weight + edges positioning (explained with example)
   - `BaselineManager`: delegates to `Compactor` (rolling/facts/topics strategies)

5. **`position: "edges"` — the lost-in-the-middle guard** — Why and how highest-scored units are placed at the top and bottom of the `<memory>` block. Include `positionEdges()` logic walkthrough.

6. **How `Engine` delegates to the pipeline** — The `SetPipeline()` delegation model and fallback to legacy behavior:
   ```go
   func (e *Engine) SetPipeline(p *evolve.PipelineHolder) { e.pipeline = p }
   ```

---

### 4.3 `scoring-and-triggers.mdx` — How Performance Is Measured

**Purpose:** Explains `TaskOutcome`, the scoring formula, outcome clustering, the `Collector`, and rate limiting.

**Operator summary:** Every completed session produces a score between 0.0 and 1.0. Scores below 0.7 (configurable) can trigger evolution. The system clusters related poor outcomes together so the meta-agent sees coherent context rather than a noisy global history.

**Sections:**
1. **`TaskOutcome` struct** — Full struct with field-by-field annotations (what populates each field, where it comes from):
   ```go
   type TaskOutcome struct {
       SessionID   string
       UserID      string
       Timestamp   time.Time
       HITLDenials int           // from sess.HITLDenialCount()
       HITLApprovals int
       SteerCount  int           // from sess.SteerCount()
       FinalStatus string        // session state at completion
       TokensUsed  int
       ToolCalls   int
       Duration    time.Duration
       TaskKeywords []string     // from ExtractKeywords(task)
       TaskTopics   []string     // from ExtractTopics(task)
       SkillsUsed   []string
       PipelineID   string       // config version at time of task
   }
   ```

2. **The scoring formula** — Annotated formula with rationale for each weight. Note: `Score()` is a pointer-receiver method (`func (o *TaskOutcome) Score() float64`):
   ```
   score = 1.0
   − (HITLDenials × 0.25)   // strongest signal: human overrode the agent
   − (SteerCount × 0.20)    // human redirected mid-task
   − 0.30 if aborted        // task abandoned
   − 0.20 if errored        // unrecoverable failure
   − 0.10 if tokens > 50k   // inefficient context use
   − 0.10 if tools > 30     // excessive tool calls
   floor: 0.0
   ```
   :::note Why no positive signals? The baseline is "worked perfectly" (1.0). Only friction events reduce it.:::

3. **Where outcomes are stored** — JSONL path pattern: `{vaultPath}/metrics/{userId}-YYYY-MM-DD.jsonl`. Note: `vaultPath` is the value passed to `NewCollector()`, typically the main vault root. JSON wire format example (include custom Duration string encoding).

4. **Outcome clustering** — Why global history is noisy; topic-scoped clusters; connected-components algorithm; keyword fallback. With small example: 5 outcomes, 2 clusters.

5. **The `Collector`** — `Record()` flow diagram: persist → score check → rate limit check → trigger. Full `Collector` struct with field annotations.

6. **Rate limiting** — Cooldown + daily cap; in-memory (resets on restart); operator tuning guide.

7. **How session manager wires in the collector** — `session.Manager.SetMetricsCollector()` (the method lives on `session.Manager`, not on `Collector`) and the `Record()` call at task completion with the actual integration snippet from `session/manager.go`.

---

### 4.4 `evolution-loop.mdx` — How the System Improves Itself

**Purpose:** The most important developer page. Full MetaAgent lifecycle, prompt construction, proposal format, and Applier behavior.

**Operator summary:** When a trigger fires, a dedicated read-only AI session analyzes the poor-scoring cluster, proposes targeted changes, and a Go applier writes them to disk — all without stopping the server. You see every change in `git log` and `evolution-log.jsonl`.

**Sections:**
1. **End-to-end lifecycle diagram** — Numbered swimlane: Collector → MetaAgent.Evolve() → pi session → ParseProposal → Applier → Reload(). ASCII or mermaid diagram.

2. **`MetaAgent.Evolve()` step by step** — Annotated walkthrough of the 9 steps at `Evolve()` level. Note: snapshot, git commit, and evolution log all happen inside `Applier.Apply()`, not at the `Evolve()` level. Steps: acquire lock → load outcomes (30-day window via `LoadOutcomes`) → cluster by topic (`ClusterOutcomes` + `FindClusterFor`) → read current config + evolved AGENTS.md section → build evolution prompt → run pi session (`runPiSession`) → parse proposal (`ParseProposal`) → call `Applier.Apply()` (which internally: snapshot → config → skills → AGENTS.md → git → log) → reload pipeline → release lock (deferred).

3. **The evolution prompt** — Full `EvolutionPrompt` struct (all 6 fields):
   ```go
   type EvolutionPrompt struct {
       SystemContext string
       Outcomes      []metrics.TaskOutcome
       CurrentConfig *evolve.PipelineConfig
       CurrentSkills []SkillSummary    // nil in current MetaAgent.Evolve() — section rendered only if non-empty
       AgentsMD      string
       Constraints   []string
   }
   ```
   `BuildEvolutionPrompt()` section breakdown; `DefaultSystemContext()` content (shown verbatim); the 5 hard constraints passed in `MetaAgent.Evolve()`. Note: `CurrentSkills` is currently always nil — the "Active Skills" section of the prompt is only rendered when non-empty.

4. **`EvolutionProposal` — the response format** — Full struct with Go signatures:
   ```go
   type EvolutionProposal struct {
       Reasoning     string
       ConfigChanges *PipelineConfig
       SkillChanges  []SkillProposal
       AgentsMDPatch string
       Summary       string
   }
   type SkillProposal struct {
       Action      string   // "create" | "update" | "delete"
       Name        string   // must be "evolved-*"
       Triggers    []string
       Description string
       Content     string
   }
   ```
   Include `ParseProposal()` brace-matching logic. The meta-agent is instructed to respond with a plain JSON object (no wrapper tag). `ParseProposal()` extracts the outermost `{…}` from the response using brace-matching, so any surrounding prose is ignored.

5. **The `Applier`** — `Apply()` sequence with code-annotated steps:
   - Snapshot first (**best-effort**: failure logs a warning and continues — not a hard abort)
   - `ApplyConfig()` — YAML write to `vault/memory/evolved/pipeline-config.yaml` (if `ConfigChanges != nil`)
   - `ApplySkill()` — namespace enforcement, directory management (per-skill, failures are warned not fatal)
   - `ApplyAgentsMD()` — marker extraction and replacement (failure is warned not fatal)
   - `gitCommit()` — lazy init, stage all, commit with `"evolve: "` prefix
   - `logEvolution()` — appends 3-field inline struct `{timestamp, summary, reasoning}` to `vault/memory/evolved/evolution-log.jsonl`

6. **AGENTS.md marker protection** — Show exact marker syntax, what happens when markers are absent (appended), what the region looks like in practice. Include `:::danger` callout: do not remove markers manually.

7. **The read-only pi session** — Why the meta-agent has no write/edit/bash tools; why the Go applier does all writes; token budget enforcement.

---

### 4.5 `versioning-and-safety.mdx` — Snapshots, Logs, and Recovery

**Purpose:** Operator-facing recovery procedures + developer-facing audit trail structures.

**Operator summary:** Every evolution attempts a snapshot before writing anything (best-effort — a snapshot failure is logged but does not block the evolution). Snapshots live in `vault/memory/evolved/snapshots/`. You can roll back by copying a snapshot's `pipeline-config.yaml` back into place and restarting the server.

**Sections:**
1. **The vault structure** — Full annotated tree of `vault/memory/evolved/`.

2. **`Snapshotter`** — `Take()` process, what gets snapshotted: `pipeline-config.yaml` (always, if present) and `agents-md-section.md` (only if `<!-- EVOLVED:START/END -->` markers exist in AGENTS.md — absent in early snapshots before any AGENTS.md evolution). Directory naming: `YYYYMMDD-HHMMSS`. `SnapshotRetainMax` is a config field reserved for future pruning logic — `Snapshotter.Take()` does not currently enforce it.

3. **`evolution-log.jsonl`** — Written by `Applier.logEvolution()` as an append-only JSONL file. Each line is a 3-field JSON object:
   ```json
   {"timestamp": "2026-03-16T14:23:01Z", "summary": "...", "reasoning": "..."}
   ```
   Note: `version.LogEntry` (with 5 fields: `Timestamp`, `SessionID`, `Score`, `Summary`, `ConfigVersion`) exists as a richer type but is not currently used by the Applier — the writer uses an inline anonymous struct with only `timestamp`, `summary`, and `reasoning`. Show how to read it (`cat evolution-log.jsonl | jq`) with an example output.

4. **Rolling back an evolution (operator guide)** — Step-by-step:
   1. Find the snapshot to restore: `ls vault/memory/evolved/snapshots/`
   2. Copy config back: `cp snapshots/20260316-142301/pipeline-config.yaml vault/memory/evolved/`
   3. Restart server (pipeline reloads on startup)
   4. Optionally revert AGENTS.md evolved section from snapshot

5. **Disabling evolution** — Set `evolution.enabled: false` in config. What happens to existing evolved config (kept, used; just no new evolutions).

6. **Git audit trail** — Every evolution produces a git commit in the vault directory. Show example `git log --oneline` output. Explain how to diff two evolutions.

7. **Constraints that code enforces** — Table distinguishing hard vs best-effort invariants:
   | Constraint | Enforced by | Strength |
   |---|---|---|
   | AGENTS.md markers: absent → patch appended; present → only region replaced | `Applier.ApplyAgentsMD()` | Best-effort at call site (failure is warned, not fatal) |
   | Evolved skills prefixed `evolved-` | `Applier.ApplySkill()` | Hard (returns error) |
   | Snapshot attempted before writes | `Applier.Apply()` | Best-effort (failure is warned, not fatal) |
   | One evolution at a time | `MetaAgent.mu` mutex | Hard (blocks concurrent callers) |
   | `position: "edges"` always set | `MergeWithDefaults()` | Hard (overwrites evolved value) |

---

### 4.6 `configuration.mdx` — Configuration Reference

**Purpose:** Complete reference for all MemEvolve-related config fields with defaults, types, and tuning guidance.

**Operator summary:** MemEvolve is configured in two places: `agentloop.yaml` (server-level toggles and limits) and `vault/memory/evolved/pipeline-config.yaml` (the pipeline strategy, evolved automatically).

**Sections:**
1. **`EvolutionConfig` fields** — Table with: field name, YAML key, type, default, description, tuning guidance. All fields are nested under the `evolution:` key in `agentloop.yaml`.

   | Field | YAML Key | Type | Default | Description |
   |---|---|---|---|---|
   | `Enabled` | `enabled` | bool | `true` | Master switch |
   | `ScoreThreshold` | `score_threshold` | float64 | `0.7` | Scores below this trigger evolution |
   | `MetaTokenBudget` | `meta_token_budget` | int | `10000` | Hard token cap for meta-agent session |
   | `MaxOutcomesPerRun` | `max_outcomes_per_run` | int | `10` | Max outcomes loaded per evolution |
   | `SnapshotRetainMax` | `snapshot_retain_max` | int | `50` | Max snapshots kept |
   | `PipelineConfigPath` | `pipeline_config_path` | string | `""` | Override evolved config path |
   | `MinCooldownSeconds` | `min_cooldown_seconds` | int | `300` | Min seconds between evolutions |
   | `MaxDailyRuns` | `max_daily_runs` | int | `10` | Max evolution runs per day |

2. **`PipelineConfig` fields** — Subsections per block: `encoder`, `retriever`, `manager`, `storer`. For each field: key, type, default, valid values, effect.

3. **Sample `agentloop.yaml` evolution block** — Complete annotated YAML snippet.

4. **Sample `pipeline-config.yaml`** — Baseline defaults as a complete YAML file with inline comments.

5. **Tuning guide** — Operator advice:
   - Lower `score_threshold` to trigger less often (e.g., 0.5 = only very bad sessions)
   - Raise `min_cooldown_seconds` to reduce LLM cost
   - `max_outcomes_per_run: 5` for focused evolution, `20` for broader context
   - Start with `enabled: false`, observe scores in the log, then enable

---

### 4.7 `custom-encoder.mdx` — Tutorial: Custom Encoder

**Purpose:** End-to-end tutorial showing a developer how to implement a custom `Encoder`, wire it into the `Pipeline`, and test it. Full reference implementation included.

**Audience:** Developer only (no operator summary needed — this page is explicitly implementation-focused).

**Sections:**
1. **When to write a custom Encoder** — Examples: domain-specific keyword extraction, embedding-based semantic encoding, multi-language support.

2. **The `Encoder` interface** — Restate the interface and all input/output types with full annotations.

3. **Step 1: Implement the interface** — Complete Go implementation:
   ```go
   package myencoder

   import (
       "context"
       "strings"
       "github.com/user/agentloop/internal/memory/evolve"
   )

   type KeywordCountEncoder struct {
       maxKeywords int
   }

   func NewKeywordCountEncoder(max int) *KeywordCountEncoder {
       return &KeywordCountEncoder{maxKeywords: max}
   }

   func (e *KeywordCountEncoder) Encode(
       ctx context.Context,
       input evolve.EncoderInput,
   ) (*evolve.EncoderOutput, error) {
       units := []evolve.MemoryUnit{}
       // ... full implementation shown
       return &evolve.EncoderOutput{Units: units}, nil
   }
   ```

4. **Step 2: Wire into `Pipeline`** — Show `NewPipeline()` call with custom encoder replacing baseline.

5. **Step 3: Write tests** — Full test file using `t.TempDir()` and the `Encode()` method directly. Covers happy path and empty input.

6. **Step 4: Plug into `PipelineHolder`** — How to initialize `PipelineHolder` with the custom implementation so `Engine.SetPipeline()` picks it up.

7. **Common pitfalls** — `:::caution` blocks for:
   - Empty `Keywords` slice causes retriever scoring to fall back to recency-only
   - `Score` field on `MemoryUnit` must be left at 0.0 from the Encoder (set by Retriever)
   - `Role` must be `"user"`, `"assistant"`, or `"system"` — other values are filtered

---

## 5. Cross-Link Additions

### 5.1 `architecture/memory-system.mdx`

Add at the end of the existing memory engine section:

```mdx
:::tip Self-evolving memory
AgentLoop includes MemEvolve, a pipeline that observes task outcomes and improves memory retrieval automatically. See [The Memory Pipeline](/memevolve/how-memory-works) for architecture details.
:::
```

### 5.2 `reference/configuration-reference.mdx`

Add in the configuration tables section after the `memory` block:

```mdx
:::tip Evolution configuration
For the full `evolution:` configuration block and `pipeline-config.yaml` reference, see the [MemEvolve Configuration](/memevolve/configuration) page.
:::
```

---

## 6. File List

```
agentloop-docs/src/content/docs/memevolve/
├── overview.mdx
├── how-memory-works.mdx
├── scoring-and-triggers.mdx
├── evolution-loop.mdx
├── versioning-and-safety.mdx
├── configuration.mdx
└── custom-encoder.mdx
```

Modified files:
```
agentloop-docs/src/content/docs/architecture/memory-system.mdx
agentloop-docs/src/content/docs/reference/configuration-reference.mdx
agentloop-docs/astro.config.mjs
```

---

## 7. Content Constraints

- All Go type signatures must be copied verbatim from source — no paraphrasing of struct fields
- All YAML config examples must use the actual field names from `config.go`
- Vault paths must match the implemented structure exactly
- `position: "edges"` must be documented as a hard constraint that cannot be overridden by evolution (enforced in `MergeWithDefaults()`)
- The scoring formula weights (0.25, 0.20, 0.30, 0.20, 0.10, 0.10) must appear exactly as in `outcome.go`
- The AGENTS.md marker strings (`<!-- EVOLVED:START -->`, `<!-- EVOLVED:END -->`) must appear verbatim
- All `:::danger` callouts must reflect actual code-enforced constraints, not recommendations

---

## 8. Out of Scope

- Documenting non-MemEvolve memory system internals (covered by existing `guides/memory-and-profiles.mdx`)
- Rust client or Slack bridge integrations with MemEvolve
- Performance benchmarks
- Comparison with external memory systems
