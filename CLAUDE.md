# AgentLoop — Development Guidelines

## What Is This Project

AgentLoop is the **single source of truth** for all agent intelligence. It wraps **pi** (`@mariozechner/pi-coding-agent` v0.54.0) as its underlying coding agent runtime and adds: persistent memory, context management, prompt caching, compaction strategies, HITL gating, vault persistence, skill management, self-evolving memory pipeline (MemEvolve), and multi-client support via Unix socket API.

**Two binaries:**
- **`agentloop-server`** — Long-running Go server (Unix socket). Manages sessions, memory, skills, HITL routing, vault persistence.
- **`agentloop`** — Thin CLI client. Connects to the server socket, sends tasks, renders output.

**Two layers within the server:**
- **Go layer** — Server, session manager, agent core, memory engine, skills registry, vault, config, pi process management, security validation
- **TypeScript layer** — pi extensions for defense-in-depth security enforcement (security-policy.ts, docker-guard.ts)

---

## Architecture

```
┌─────────────┐  ┌──────────────┐  ┌──────────────┐
│  CLI Client  │  │ Slack Bridge │  │  Future: Web │
│  (Go TUI)    │  │ (Node.js)    │  │  UI, Mobile  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       └────────┬────────┘────────┬────────┘
                │  Unix Socket    │
                │  (JSON-RPC)     │
                ▼                 ▼
┌────────────────────────────────────────────────────┐
│              AgentLoop Server (Go)                  │
│                                                     │
│  ┌────────────┐  ┌────────────┐  ┌──────────────┐ │
│  │ Socket API │  │ Session    │  │ Memory       │ │
│  │ (JSON-RPC) │  │ Manager    │  │ Engine       │ │
│  │            │  │ • lifecycle│  │ • profiles   │ │
│  │ • task     │  │ • limits   │  │ • conv log   │ │
│  │ • steer    │  │ • routing  │  │ • compaction │ │
│  │ • abort    │  │            │  │ • caching    │ │
│  │ • hitl_res │  │            │  │              │ │
│  │ • sessions │  │            │  │              │ │
│  │ • memory   │  │            │  │              │ │
│  └─────┬──────┘  └─────┬──────┘  └──────┬───────┘ │
│        │               │                │          │
│  ┌─────▼───────────────▼────────────────▼────────┐ │
│  │              Agent Core                        │ │
│  │  • Builds prompts (memory + task + skills)     │ │
│  │  • Manages pi subprocess via RPC               │ │
│  │  • HITL gate (routes to client via socket)     │ │
│  │  • Stuck detection, resource limits            │ │
│  │  • Context window management + compaction      │ │
│  └──────────────────────┬────────────────────────┘ │
│                         │ stdin/stdout JSON RPC     │
│  ┌──────────────────────▼────────────────────────┐ │
│  │  pi coding agent (subprocess per session)      │ │
│  │  • 15+ LLM providers                          │ │
│  │  • read/write/edit/bash/grep/find/ls tools     │ │
│  │  • Extensions (security-policy, docker-guard)  │ │
│  └────────────────────────────────────────────────┘ │
│                                                     │
│  ┌──────────────────────────────────────────────┐  │
│  │  Vault (~/.local/share/agentloop/vault/)      │  │
│  │  ├── sessions/     (session transcripts)      │  │
│  │  ├── memory/                                  │  │
│  │  │   ├── users/    (per-user profiles)        │  │
│  │  │   ├── contexts/ (conversation summaries)   │  │
│  │  │   ├── cache/    (prompt cache)             │  │
│  │  │   └── evolved/  (MemEvolve artifacts)      │  │
│  │  └── skills/       (on-demand skill files)    │  │
│  └──────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘
```

### Unix Socket API (JSON-RPC 2.0)

Socket path: `~/.local/share/agentloop/agentloop.sock`

**Client → Server (Requests):**

| Method | Params | Description |
|--------|--------|-------------|
| `task.start` | `{userId, text, workDir?, source}` | Start a new agent task |
| `task.steer` | `{sessionId, text}` | Redirect a running task |
| `task.abort` | `{sessionId}` | Abort a running task |
| `hitl.respond` | `{sessionId, requestId, decision}` | Approve/deny/abort HITL request |
| `session.list` | `{userId?, status?}` | List sessions |
| `memory.get` | `{userId}` | Get user's memory context |
| `memory.update` | `{userId, key, value}` | Update a user fact |
| `health.check` | `{}` | Server health + active session count |

**Server → Client (Notifications):**

| Method | Params | Description |
|--------|--------|-------------|
| `event.text` | `{sessionId, content}` | Agent text output chunk |
| `event.tool_use` | `{sessionId, toolName, input}` | Tool about to execute |
| `event.tool_result` | `{sessionId, toolName, output, success}` | Tool result |
| `event.hitl_request` | `{sessionId, requestId, toolName, details, options}` | HITL approval needed |
| `event.done` | `{sessionId, output, stats}` | Task completed |
| `event.error` | `{sessionId, message}` | Task error |
| `event.session_saved` | `{sessionId}` | Session persisted to vault |

### Data Flow for a Task

```
Client sends: {"jsonrpc":"2.0","id":1,"method":"task.start","params":{"userId":"marco","text":"fix tests","source":"cli"}}
    │
    ▼
server/handler.go → session.Manager.StartSession()
    │
    ├─ 1. Enforce limits (MaxConcurrent, MaxPerUser)
    ├─ 2. Create Session (id=sess-{uuid8}, state=starting)
    ├─ 3. Subscribe client to session events
    ├─ 4. Launch agent goroutine:
    │      agent.Core.Run(ctx, memoryContext, task, session)
    │      ├─ Builds prompt: <memory>...</memory> + task text
    │      ├─ Creates PiBridge, registers event + HITL handlers
    │      ├─ bridge.Start() launches pi subprocess (env sanitized)
    │      ├─ bridge.Prompt() sends task
    │      └─ Event loop: done/abort/steer/ctx.Done()
    ├─ 5. Events broadcast to subscribed clients via socket
    ├─ 6. HITL requests → event.hitl_request → client → hitl.respond → session resolution
    ├─ 7. On completion: save to vault, update memory, broadcast event.done
    └─ 8. Cleanup session from manager
```

### HITL Approval Flow

```
Pi extension detects risky tool call
    │
    ▼
Pi sends: {"type":"extension_ui_request","id":"req-1","method":"confirm","title":"..."}
    │
    ▼
bridge.readEvents() → onHITL handler in agent.Core
    │
    ▼
Agent core generates requestId, calls session.SetPendingHITL()
    │
    ▼
Callback broadcasts event.hitl_request to subscribed clients
    │
    ▼
Client (CLI/Slack) shows HITL prompt to user
    │
    ▼
Client sends: {"jsonrpc":"2.0","method":"hitl.respond","params":{"decision":"approve"}}
    │
    ▼
handler → session.ResolveHITL() → agent.Core.WaitHITL() unblocks
    │
    ▼
bridge sends extension_ui_response back to pi
```

---

## Directory Structure

```
agentloop/
├── cmd/
│   ├── agentloop-server/main.go    # Long-running server (Unix socket)
│   └── agentloop/main.go           # CLI client (connects to server)
│
├── internal/
│   ├── server/
│   │   ├── server.go               # Unix socket listener, JSON-RPC dispatch
│   │   ├── handler.go              # Request handlers (task, session, memory, hitl)
│   │   └── client.go               # Connected client state + notification push
│   │
│   ├── session/
│   │   ├── manager.go              # Session lifecycle, limits, routing
│   │   └── session.go              # Session state machine
│   │
│   ├── agent/
│   │   ├── core.go                 # Agent core: builds prompts, manages pi, HITL
│   │   └── prompt_builder.go       # Assembles prompt: memory + task
│   │
│   ├── bridge/
│   │   ├── rpc.go                  # Pi subprocess RPC client
│   │   ├── events.go               # Pi event/command types
│   │   └── rpc_test.go
│   │
│   ├── memory/
│   │   ├── engine.go               # Memory engine: orchestrates all memory ops
│   │   ├── scheduler.go            # MemScheduler: Minimal/Standard/Detailed routing
│   │   ├── index.go                # Conversation indexing, ExtractKeywords/Topics
│   │   ├── profile.go              # Per-user profile (preferences, facts, patterns)
│   │   ├── conversation.go         # Conversation log (per-user, per-day)
│   │   ├── compaction.go           # Compaction strategies + LLM delta extraction
│   │   ├── cache.go                # Prompt cache (stable prefix optimization)
│   │   ├── notes/                  # Zettelkasten atomic note store
│   │   │   ├── store.go            # AtomicNote type + NoteStore interface (test-only)
│   │   │   └── sqlite.go           # SQLiteNoteStore (production, per-user DBs)
│   │   ├── llm/                    # Background LLM operations for memory
│   │   │   └── client.go           # LLMClient interface, PiCompletionClient, NoopClient
│   │   └── evolve/                 # MemEvolve: self-evolving memory pipeline
│   │       ├── interfaces.go       # Encoder, Storer, Retriever, Manager interfaces
│   │       ├── pipeline.go         # Pipeline orchestrator + atomic hot-swap
│   │       ├── config.go           # PipelineConfig + YAML load/save
│   │       ├── config_test.go
│   │       ├── baseline/           # Baseline impls wrapping existing engine
│   │       │   ├── encoder.go
│   │       │   ├── storer.go
│   │       │   ├── retriever.go
│   │       │   ├── manager.go
│   │       │   └── baseline_test.go
│   │       ├── metrics/            # TaskOutcome scoring, clustering, Collector
│   │       │   ├── outcome.go
│   │       │   ├── outcome_test.go
│   │       │   ├── collector.go
│   │       │   ├── collector_test.go
│   │       │   ├── cluster.go
│   │       │   └── cluster_test.go
│   │       ├── meta/               # MetaAgent, Applier, proposal types, prompt
│   │       │   ├── proposal.go
│   │       │   ├── prompt.go
│   │       │   ├── agent.go
│   │       │   ├── applier.go
│   │       │   └── applier_test.go
│   │       └── version/            # Snapshot + EvolutionLog (JSONL)
│   │           ├── snapshot.go
│   │           ├── log.go
│   │           └── version_test.go
│   │
│   ├── pirun/
│   │   └── pirun.go                # Shared pi session runner — RunTextSession()
│   │
│   ├── skills/
│   │   ├── registry.go             # Skill registry (name → tags + instructions + files)
│   │   └── agent.go                # SkillAgent — LLM-driven skill selection side process
│   │
│   ├── vault/
│   │   ├── vault.go                # Vault directory management
│   │   ├── session.go              # Read/write session markdown
│   │   └── frontmatter.go          # YAML frontmatter parser
│   │
│   ├── security/
│   │   ├── policy.go               # Path validation, SSRF, docker guards
│   │   └── policy_test.go
│   │
│   ├── config/
│   │   ├── config.go               # Full config struct
│   │   └── loader.go               # Viper loader
│   │
│   ├── errors/
│   │   └── errors.go               # Error taxonomy
│   │
│   └── logging/
│       └── logging.go              # Structured slog logging
│
├── extensions/                     # Pi extensions (loaded by pi subprocess)
│   ├── security-policy.ts
│   ├── docker-guard.ts
│   ├── memory-tools.ts             # Add_memory, Update_memory, Delete_memory, Retrieve_memory tools
│   └── skill-tools.ts              # Find_skill tool for LLM-driven skill selection
│
├── agents-md/
│   └── AGENTS.md                   # Instructions pi loads for agent behavior
├── configs/
│   └── agentloop.yaml
└── go.mod
```

---

## Prerequisites

- **Go** >= 1.23.0
- **pi** v0.54.0+ installed globally: `npm install -g @mariozechner/pi-coding-agent`
- Verify with: `pi --version`

---

## Build & Run

```bash
# Build both binaries
go build -o agentloop-server ./cmd/agentloop-server
go build -o agentloop ./cmd/agentloop

# Build with version tag
go build -ldflags="-X main.version=v1.0.0" -o agentloop-server ./cmd/agentloop-server

# Start the server
./agentloop-server &

# Run a task via CLI
./agentloop "describe the task here"

# Health check via nc (netcat) — socat also works
echo '{"jsonrpc":"2.0","id":1,"method":"health.check","params":{}}' | nc -U ~/.local/share/agentloop/agentloop.sock

# Tidy modules after adding dependencies
go mod tidy
```

---

## Testing

```bash
# Run ALL tests (do this before any PR/commit)
go test ./...

# Run specific package tests
go test ./internal/bridge/... -v
go test ./internal/security/... -v
go test ./internal/memory/... -v
go test ./internal/memory/evolve/... -v

# Security tests MUST always pass — these are mandatory
go test ./internal/security/... -v
go test ./internal/bridge/... -v
```

### Existing Tests

| Test | File | Verifies |
|------|------|----------|
| `TestBuildSafeEnv` | `internal/bridge/rpc_test.go` | API keys stripped from pi subprocess env |
| `TestRPCCommandSerialization` | `internal/bridge/rpc_test.go` | JSON protocol correctness — verifies `"message"` field used, not `"text"` |
| `TestExtensionUIResponseSerialization` | `internal/bridge/rpc_test.go` | HITL response format |
| `TestValidatePathTraversal` | `internal/security/policy_test.go` | `../../etc/passwd` blocked |
| `TestValidatePathAllowed` | `internal/security/policy_test.go` | Valid paths permitted |
| `TestValidateDockerBlocked` | `internal/security/policy_test.go` | Dangerous volume mounts rejected |
| `TestValidateDockerSubcommand` | `internal/security/policy_test.go` | Disallowed subcommands rejected |
| `TestPipelineConfigLoad` | `internal/memory/evolve/config_test.go` | PipelineConfig YAML loading |
| `TestPipelineConfigMergeDefaults` | `internal/memory/evolve/config_test.go` | Missing fields filled from defaults |
| `TestBaselineEncoderWrapsExisting` | `internal/memory/evolve/baseline/baseline_test.go` | Encoder extracts keywords/topics |
| `TestBaselineRetrieverWrapsExisting` | `internal/memory/evolve/baseline/baseline_test.go` | Retriever matches existing engine behavior |
| `TestPipelineReloadAtomicSwap` | `internal/memory/evolve/baseline/baseline_test.go` | Baseline pipeline reload isolates old sessions |
| `TestScorePerfectTask` | `internal/memory/evolve/metrics/outcome_test.go` | Perfect task scores 1.0 |
| `TestScoreHITLDenials` | `internal/memory/evolve/metrics/outcome_test.go` | Each denial deducts 0.25 |
| `TestScoreSteers` | `internal/memory/evolve/metrics/outcome_test.go` | Each steer deducts 0.20 |
| `TestScoreAbortedTask` | `internal/memory/evolve/metrics/outcome_test.go` | Aborted status deducts 0.30 |
| `TestScoreFloor` | `internal/memory/evolve/metrics/outcome_test.go` | Score never goes below 0.0 |
| `TestClusterBySharedTopic` | `internal/memory/evolve/metrics/cluster_test.go` | Outcomes grouped by shared topic |
| `TestClusterConnectedComponents` | `internal/memory/evolve/metrics/cluster_test.go` | Transitive topic links form one cluster |
| `TestClusterNoTopicsFallsBackToKeywords` | `internal/memory/evolve/metrics/cluster_test.go` | Keyword overlap used when topics absent |
| `TestCollectorPersist` | `internal/memory/evolve/metrics/collector_test.go` | Outcomes written to JSONL |
| `TestCollectorRateLimitingCooldown` | `internal/memory/evolve/metrics/collector_test.go` | Cooldown blocks re-trigger |
| `TestCollectorRateLimitingDailyCap` | `internal/memory/evolve/metrics/collector_test.go` | Daily cap respected |
| `TestApplierAgentsMDMarkerProtection` | `internal/memory/evolve/meta/applier_test.go` | Only `EVOLVED:START/END` section overwritten |
| `TestApplierAgentsMDCreatesMarkers` | `internal/memory/evolve/meta/applier_test.go` | Markers appended when absent from AGENTS.md |
| `TestApplierSkillNamespacing` | `internal/memory/evolve/meta/applier_test.go` | Evolved skills prefixed `evolved-` |
| `TestApplierSnapshotCreated` | `internal/memory/evolve/meta/applier_test.go` | Snapshot dir created before apply |
| `TestApplierGitNotInstalled` | `internal/memory/evolve/meta/applier_test.go` | Apply succeeds gracefully when git absent |
| `TestProposalParsingInvalid` | `internal/memory/evolve/meta/applier_test.go` | Invalid JSON proposal returns error |
| `TestEvolutionLogAppend` | `internal/memory/evolve/version/version_test.go` | Log entries appended to JSONL |
| `TestSnapshotContainsAllFiles` | `internal/memory/evolve/version/version_test.go` | Snapshot copies all tracked files |
| `TestScheduleMinimal` | `internal/memory/scheduler_test.go` | Short task routes to Minimal context level |
| `TestScheduleStandard` | `internal/memory/scheduler_test.go` | Medium task routes to Standard context level |
| `TestScheduleDetailed` | `internal/memory/scheduler_test.go` | Long task routes to Detailed context level |
| `TestScheduleComplexityKeywords` | `internal/memory/scheduler_test.go` | Complexity keywords force Detailed regardless of word count |
| `TestInMemoryNoteStoreConformance` | `internal/memory/notes/store_test.go` | InMemoryNoteStore basic CRUD correctness |
| `TestAtomicNoteTimestamps` | `internal/memory/notes/store_test.go` | Note timestamps set on Add, preserved on Get |
| `TestListByUser` | `internal/memory/notes/store_test.go` | ListByUser returns only notes for that user |
| `TestSQLiteNoteStoreConformance` | `internal/memory/notes/store_test.go` | SQLiteNoteStore matches NoteStore interface contract |
| `TestSQLiteNoteStoreVectorSearch` | `internal/memory/notes/store_test.go` | Vector search returns cosine-nearest notes |
| `TestSQLiteNoteStoreNoEmbeddingSkipped` | `internal/memory/notes/store_test.go` | SearchByVector returns nil (no error) when embeddingDim=0 |
| `TestNoopClientEmbed` | `internal/memory/llm/client_test.go` | NoopClient.Embed returns nil slice, no error |
| `TestNoopClientComplete` | `internal/memory/llm/client_test.go` | NoopClient.Complete returns empty string, no error |
| `TestRecordInteraction_TagsContextID` | `internal/memory/engine_test.go` | Interaction tagged with conversationContextID |
| `TestRecordInteraction_EmptyContextID_GlobalBehavior` | `internal/memory/engine_test.go` | Empty contextID falls through to global conversation |
| `TestRecordInteraction_InvalidatesCacheForContext` | `internal/memory/engine_test.go` | Cache invalidated after RecordInteraction |
| `TestGetContextForUserAndConversationContext_IsolatesThread` | `internal/memory/engine_test.go` | Thread context returns only that thread's history |
| `TestGetContextForUserAndConversationContext_EmptyContextID_FallsBack` | `internal/memory/engine_test.go` | Empty contextID falls back to profile-only context |
| `TestAddNoteLinksRelatedNotes` | `internal/memory/engine_test.go` | AddNote auto-links top-3 keyword-related notes bidirectionally |
| `TestExtractDeltaWithPrefix` | `internal/memory/compaction_delta_test.go` | LLM response with "DELTA: " prefix returns extracted delta |
| `TestExtractDeltaNoPrefix` | `internal/memory/compaction_delta_test.go` | LLM response without prefix returns empty string |
| `TestExtractDeltaNilClient` | `internal/memory/compaction_delta_test.go` | Nil LLM client returns empty string (no panic) |
| `TestExtractDeltaEmptyResponse` | `internal/memory/compaction_delta_test.go` | Empty LLM response returns empty string |
| `TestThreadIsolation_FullRoundTrip` | `internal/memory/integration_test.go` | Full record→retrieve round-trip preserves thread isolation |
| `TestThreadIsolation_NewThread_GetsOnlyProfile` | `internal/memory/integration_test.go` | New thread context contains only profile, no history bleed |
| `TestRegistryCatalog` | `internal/skills/registry_test.go` | `Catalog()` returns all skills as `SkillCatalogEntry` (no instructions) |
| `TestSkillLoadWithFiles` | `internal/skills/registry_test.go` | Auto-scan files, frontmatter description inheritance, absolute paths set |
| `TestSkillFileNoExtension` | `internal/skills/registry_test.go` | Files with no extension get `Type: ""` and are included |
| `TestSkillTagsMigration` | `internal/skills/registry_test.go` | Old `triggers` field silently ignored; `tags` parsed correctly |
| `TestParseSkillResponseMatch` | `internal/skills/agent_test.go` | SkillAgent returns correct skill name |
| `TestParseSkillResponseNone` | `internal/skills/agent_test.go` | "none" response returns empty string |
| `TestSkillAgentFindEmptyCatalog` | `internal/skills/agent_test.go` | Empty catalog short-circuits without calling pi |
| `TestSkillAgentConfigDefaults` | `internal/config/config_test.go` | SkillAgentConfig fields default to empty (inherit from pi) |
| `TestSkillToolHandlerRegistration` | `internal/bridge/rpc_test.go` | SetSkillToolHandler registers callback |
| `TestRunTextSessionSignature` | `internal/pirun/pirun_test.go` | RunTextSession signature and basic behavior |

### Test Conventions

- Tests live in `*_test.go` in the same package (not a separate `_test` package).
- Security tests use `t.Fatal("SECURITY: ...")` for critical violations.
- Use `t.TempDir()` for filesystem tests.
- Test names: `TestFunctionNameBehavior` (e.g., `TestValidatePathTraversal`).
- When adding a new feature, write tests in the same package. Run only the relevant package tests during development, then `go test ./...` before finishing.

---

## Go Module & Dependencies

**Module path:** `github.com/user/agentloop`

**All internal imports use the full module path:**
```go
import "github.com/user/agentloop/internal/agent"
import "github.com/user/agentloop/internal/bridge"
import "github.com/user/agentloop/internal/config"
import "github.com/user/agentloop/internal/errors"
import "github.com/user/agentloop/internal/logging"
import "github.com/user/agentloop/internal/memory"
import "github.com/user/agentloop/internal/memory/llm"
import "github.com/user/agentloop/internal/memory/notes"
import "github.com/user/agentloop/internal/memory/evolve"
import "github.com/user/agentloop/internal/memory/evolve/baseline"
import "github.com/user/agentloop/internal/memory/evolve/metrics"
import "github.com/user/agentloop/internal/memory/evolve/meta"
import "github.com/user/agentloop/internal/memory/evolve/version"
import "github.com/user/agentloop/internal/security"
import "github.com/user/agentloop/internal/server"
import "github.com/user/agentloop/internal/session"
import "github.com/user/agentloop/internal/pirun"
import "github.com/user/agentloop/internal/skills"
import "github.com/user/agentloop/internal/vault"
```

**Direct dependencies (go.mod):**
```
github.com/google/uuid                      v1.6.0    # UUID generation for session/client IDs
github.com/spf13/viper                      v1.21.0   # Config file parsing + env vars
gopkg.in/yaml.v3                            v3.0.1    # YAML marshal/unmarshal
github.com/mattn/go-sqlite3                 v1.14.37  # SQLite driver (requires CGo)
github.com/asg017/sqlite-vec-go-bindings    v0.1.6    # sqlite-vec vector search extension
```

Do NOT add dependencies without explicit approval. The project intentionally has a minimal dependency footprint.

---

## Configuration System

**Default config path:** `~/.config/agentloop/agentloop.yaml`
**Sample config:** `configs/agentloop.yaml`
**Override via env:** `AGENTLOOP_*` prefix (Viper auto-binding)

### Config Loading Order
1. File at default path (or path passed programmatically)
2. Environment variables with `AGENTLOOP_` prefix
3. Hardcoded defaults from `config.Defaults()`
4. If config file missing (not error), defaults are used silently

### Adding a New Config Field

1. Add the field to the appropriate struct in `internal/config/config.go` with `mapstructure` tag
2. Set the default value in `Defaults()`
3. Add the field to `configs/agentloop.yaml` as documentation
4. If the field needs path expansion (`~/`), add `expandHome()` call in `Load()`
5. Access it via `cfg.Section.Field` wherever `*config.Config` is available

### Config Sections Reference

| Section | Struct | Purpose |
|---------|--------|---------|
| `server` | `ServerConfig` | Unix socket path |
| `pi` | `PiConfig` | Pi binary path, provider, model, extensions dir, extra args |
| `vault` | `VaultConfig` | Vault storage path |
| `memory` | `MemoryConfig` | Profile entries limit, conversation retention, compaction settings, cache TTL, `embedding_dims` for vector search |
| `memory.agent` | `MemoryAgentConfig` | Background LLM for delta extraction: `enabled`, `binary`, `provider`, `model` (use a cheap model like Haiku) |
| `sessions` | `SessionConfig` | Max concurrent, max per user, timeout, token budget, tool call limit, stuck threshold, LRU eviction |
| `hitl` | `HITLConfig` | Always-pause tools, timeout, timeout action |
| `security` | `SecurityConfig` | Allowed paths, blocked env prefixes, blocked CIDRs, docker rules, **injection protection** |
| `skills` | `SkillsConfig` | Skill directory paths; `agent` sub-config for SkillAgent (binary, provider, model) |
| `logging` | `LoggingConfig` | Log level, log file path |
| `evolution` | `EvolutionConfig` | MemEvolve: enabled, score threshold, meta token budget, rate limiting, snapshot retention |

---

## Package-by-Package Guide

### `internal/server` — Unix Socket Server

JSON-RPC 2.0 server over Unix domain socket. Manages client connections and dispatches requests.

**Key types:**
- `Server` — socket listener, client tracking, broadcast
- `Client` — per-connection state, session subscriptions, mutex-protected writes
- `Handler` — routes all JSON-RPC methods to session manager and memory engine

**Important:** Socket has 0700 permissions (owner-only access).

### `internal/session` — Session Management

**`Manager`** — enforces concurrency limits, starts agent goroutines, routes steer/abort/HITL, cleans up on completion.

**`Session`** — state machine with states: `starting` → `running` → `waiting_hitl` → `done`/`aborted`/`error`. Uses channels for steer, abort, and HITL resolution.

**Key patterns:**
- Session IDs are UUID-based: `sess-{uuid8}`
- Per-user session limits enforced in `StartSession()`
- HITL resolution via channels: `SetPendingHITL()` → `WaitHITL()` → `ResolveHITL()`
- LRU eviction: when `max_concurrent` is reached and `evict_lru: true`, `evictOldestLRU()` aborts the session with the oldest `LastActivity` and removes it from maps immediately; `LastActivity` is updated via `Touch()` on every `OnText` callback
- HITL denial and steer counters (`HITLDenialCount()`, `SteerCount()`) are incremented atomically and read by the metrics `Collector` at task completion

### `internal/agent` — Agent Core

**`Core`** — wraps PiBridge into a `Run()` method. Builds prompts with memory prefix, manages event loop (done/abort/steer/ctx), collects stats.

**`Run()` event loop** selects on four channels:
- `doneCh` — closed by `agent_end` event from pi (or on streaming error/retry failure)
- `b.Done()` — closed when pi process exits; handles crashes where `agent_end` is never emitted
- `sess.AbortCh()` — user abort signal
- `ctx.Done()` — server shutdown

Early failures (`b.Start()` or `b.Prompt()` errors) call `OnError` before returning, so the client always receives an `event.error` notification instead of hanging.

**`PromptBuilder`** — assembles memory context + skills + task into a single prompt. Optimized for Anthropic prompt caching (stable prefix first, dynamic content last).

**`Callbacks`** — event routing struct: OnText, OnToolUse, OnToolResult, OnHITLRequest, OnDone, OnError.

**`SessionInterface`** — interface for decoupling from session package: `WaitHITL()`, `AbortCh()`, `SteerCh()`.

### `internal/bridge` — Pi RPC Bridge (CORE)

This is the most critical package. It manages the pi subprocess and all communication.

**Key type:** `PiBridge` — manages pi as a child process over JSON-line RPC.

**Lifecycle:**
```
New(piCfg, secCfg) → SetEventHandler() → SetHITLHandler() → Start(ctx, workDir) → Prompt() → <-Done() → Stop()
```

**Pi RPC protocol (stdin → pi):**

| Command | JSON sent | Description |
|---------|-----------|-------------|
| Prompt | `{"type":"prompt","id":"task","message":"..."}` | Send task to agent |
| Steer | `{"type":"steer","message":"...","streamingBehavior":"steer"}` | Interrupt mid-run |
| Abort | `{"type":"abort"}` | Stop current run |

Note: the prompt field is `"message"`, **not** `"text"`. Using `"text"` silently fails — pi ignores it.

**Pi RPC protocol (pi → stdout events):**

| Pi event type | Mapped to | Description |
|---------------|-----------|-------------|
| `message_update` + `assistantMessageEvent.type == "text_delta"` | `OnText(delta)` | Streaming text chunk |
| `tool_execution_start` | `OnToolUse(toolName, args)` | Tool invocation started |
| `tool_execution_end` | `OnToolResult(toolName, output, success)` | Tool finished |
| `agent_end` | `closeDone()` → `OnDone` | Agent completed (success or failure) |
| `auto_retry_end` with `success==false` | `OnError` + `closeDone()` | Failed after all retries |
| `message_update` + `assistantMessageEvent.type == "error"` | `OnError` + `closeDone()` | Streaming error |
| `extension_ui_request` | HITL handler | Permission gate request |

**Extension UI responses** are sent as raw `ExtensionUIResponse` JSON via `sendJSON()`, not wrapped in `RPCCommand`.

**Important implementation details:**
- Scanner buffer is 10MB max line (for large tool outputs)
- `buildSafeEnv()` strips env vars matching `BlockedEnvPrefixes` before subprocess creation
- Extensions auto-loaded from `ExtensionsDir` (all `.ts`/`.js` files)
- If `ExtensionsDir` is empty, auto-detects `{binary-dir}/extensions/`
- `sendJSON()` is mutex-protected; `SendCommand()` delegates to it
- `core.Run()` listens on both `doneCh` (from `agent_end` event) and `b.Done()` (pi process exit) — the latter handles pi crashes where no `agent_end` is emitted

### `internal/memory` — Memory Engine

All memory, caching, compaction, and context management lives here. No client ever manages memory.

**`Engine`** — orchestrates profile store, conversation store, notes store, prompt cache, and compactor. Key methods:
- `GetContextForUser(userId)` — legacy, no task context; uses TTL cache (5-min).
- `GetContextForUserWithTask(userId, task)` — primary retrieval path. Calls `Schedule(task)` to pick context depth (Minimal/Standard/Detailed), then retrieves notes via MemEvolve pipeline or keyword+topic fallback. Falls back to indexing-based retrieval if pipeline errors.
- `GetContextForUserAndConversationContext(userId, contextID)` — thread-scoped retrieval. Returns profile + only history from that thread. Use this for Slack-style threaded conversations.
- `RecordInteraction(userId, userMsg, agentReply, toolsUsed, conversationContextID)` — appends to conversation log, updates profile heuristically, and **asynchronously** extracts a delta atomic note via LLM if `LLMClient` is set.
- `AddNote(note)` — saves to `SQLiteNoteStore` and **auto-links to top-3 keyword-related existing notes bidirectionally**. Invalidates user cache. Always route note saves through `Engine.AddNote()`, never call the note store directly.
- `UpdateNote(note)` / `DeleteNote(userID, noteID)` — both invalidate user cache.
- `SetPipeline(p)` / `SetNoteStore(s)` / `SetLLMClient(c)` — injection methods for wiring MemEvolve pipeline, `SQLiteNoteStore`, and background LLM client.

**`MemScheduler`** (`scheduler.go`) — routes a task string to a `ContextLevel`:
- `Minimal` (≤3 words, no complexity keyword) — profile + top-3 notes only.
- `Standard` (default) — profile + moderate history.
- `Detailed` (>25 words OR contains complexity keyword) — full context.
- Complexity keywords: `refactor`, `architect`, `redesign`, `migrate`, `integrate`, `security`, `performance`, `debug`, `analyse`, `analyze`.
- Deterministic: same input always produces same level.

**`ProfileStore`** — per-user YAML profiles in `vault/memory/users/`. Tracks communication style, preferences, frequent projects, fact sheet, recent topics.

**`ConversationStore`** — per-user, per-day markdown conversation logs in `vault/memory/contexts/`. Legacy; superseded by `SQLiteNoteStore` for semantic knowledge. Still used for verbatim turn history + sidecar indexing.

**Conversation index** (`index.go`) — JSON sidecar alongside each daily markdown file. Enables fast keyword/topic scoring without parsing full history. `ExtractKeywords(content)` (up to 15, filters stopwords) and `ExtractTopics(content)` (stems against 40-item domain taxonomy) are exported; used by MemEvolve baseline implementations.

**`internal/memory/notes/`** — Zettelkasten atomic note store.
- `AtomicNote` — one idea per note: `{ID, UserID, Content, Keywords, Tags, Description, Embedding, Connections, CreatedAt, UpdatedAt}`. `Tags` maps to topic taxonomy. `Connections` holds IDs of related notes (auto-linked by Engine).
- `SQLiteNoteStore` (`sqlite.go`) — **production store**. Per-user SQLite databases at `vault/memory/notes/{userID}.db`. Keyword search via `json_each()`. Vector search via `sqlite-vec` `vec0` virtual table when `embeddingDim > 0`. Lazy DB opening per user; migration idempotent. `SearchByVector` returns nil (no error) when `embeddingDim=0` or embedding length mismatches. Always use `*SQLiteNoteStore` directly in production code.
- `NoteStore` interface (`store.go`) — **test-only**. `InMemoryNoteStore` implements it for unit tests. Never use `NoteStore` in production code.

**`internal/memory/llm/`** — background LLM operations for async memory work.
- `LLMClient` interface: `Embed(text) ([]float32, error)` + `Complete(prompt) (string, error)`. `Embed` always returns nil (pi subprocesses don't expose embedding APIs). Use `NoopClient` when `memory.agent.enabled = false`.
- `PiCompletionClient` — spawns a short-lived pi subprocess (120s timeout) with the configured cheap model (e.g. Haiku). Used for delta extraction in `RecordInteraction()`. Configured via `memory.agent.*` config section.
- Constructor: `NewPiCompletionClient(piCfg config.PiConfig)`.

**`Compactor`** (`compaction.go`) — 3 deterministic strategies (rolling, facts, topics). `extractDelta()` calls `LLMClient.Complete()` with a minimal prompt, looks for `"DELTA: "` prefix, returns empty string on noop or nil client. Delta extraction runs asynchronously in `RecordInteraction()`.

**`PromptCache`** — in-memory TTL cache for assembled context strings.

### `internal/memory/evolve` — MemEvolve Self-Evolving Pipeline

Adds a self-improving memory layer that observes task outcomes and autonomously updates the memory pipeline config, skills, and AGENTS.md when performance degrades.

**Core interfaces** (`interfaces.go`):
- `Encoder` — transform an interaction into `[]MemoryUnit` (with keywords, topics, metadata)
- `Storer` — persist/load `MemoryUnit` slices to/from vault
- `Retriever` — select relevant units for a prompt (Jaccard + topic scoring, edges positioning)
- `Manager` — compact/prune under token budget

**`MemoryUnit`** — universal exchange type: `{ID, Timestamp, Role, Content, Keywords, Topics, Metadata, Score}`

**`Pipeline`** (`pipeline.go`) — wires the four interfaces together. `PipelineHolder` holds the active `Pipeline` and supports atomic hot-swap via `Reload()`: new sessions pick up the new config while in-flight sessions continue on the old one.

**`PipelineConfig`** (`config.go`) — YAML file at `vault/memory/evolved/pipeline-config.yaml`. Controls encoder strategy, retriever scoring weights, manager compaction, storer format. Hot-reloaded by the MetaAgent after each evolution.

**`RetrievalQuery`** — `QueryEmbedding []float32` field enables vector search. When nil, only keyword search runs. Caller is responsible for computing the embedding before calling `Retrieve()`.

**`baseline/`** — baseline implementations backed by `SQLiteNoteStore`:
- `BaselineEncoder` — calls `ExtractKeywords`/`ExtractTopics` from `index.go`
- `BaselineStorer` — writes via `Engine.AddNote()` (triggers auto-linking); reads via `Engine.NoteStore().ListByUser()`. Constructor: `NewBaselineStorer(profiles, *memory.Engine)`.
- `BaselineRetriever` — dual-mode: vector search (`SearchByVector`) when `QueryEmbedding` is set, keyword search (`SearchByKeywords`) always. Both result sets are merged, deduplicated by ID, Jaccard re-ranked, and edge-positioned. Constructor: `NewBaselineRetriever(profiles, *SQLiteNoteStore, config)`.
- `BaselineManager` — delegates to `Compactor`

**`metrics/`** — task outcome collection and scoring:
- `TaskOutcome` — captures signals per session: HITL denials, steers, abort, error, tokens, tools, duration, keywords, topics, skills used, pipeline ID
- `Score()` — composite score (1.0 = perfect; each HITL denial −0.25, steer −0.20, abort −0.30, error −0.20, tokens>50k −0.10, tools>30 −0.10; floor 0.0)
- `Collector` — records outcomes to `vault/memory/evolved/metrics/{userId}-YYYY-MM-DD.jsonl`; triggers evolution callback when `score < threshold` subject to rate limiting (cooldown + daily cap)
- `ClusterOutcomes()` — groups poor outcomes by shared topics (connected-components); falls back to keywords. Ensures the MetaAgent sees coherent context.

**`meta/`** — the meta-evolution agent:
- `MetaAgent` — serialized via mutex (one evolution at a time). Constructor: `NewMetaAgent(vaultPath, agentsMDPath, skillsPath, piCfg, secCfg, evoCfg, pipeline, *memory.Engine)`. `Evolve()` loads recent poor outcomes, clusters them, builds a read-only pi session with `EvolutionPrompt`, parses `EvolutionProposal` from pi's output, then calls `Applier.Apply(proposal, outcome.UserID)`.
- `Applier` — constructor: `NewApplier(vaultPath, agentsMDPath, skillsPath, *memory.Engine)`. `Apply(proposal, userID)` applies all proposal fields in order: config → skills → AGENTS.md → notes → orchestrator patches → git commit. `ApplyNoteProposals(proposals, userID)` calls `Engine.AddNote()` for each `NoteProposal`, which triggers bidirectional auto-linking; skipped silently when engine is nil.
- `EvolutionProposal` — `{Reasoning, ConfigChanges, SkillChanges, AgentsMDPatch, NoteProposals []NoteProposal, OrchestratorPatches, Summary}`
- `NoteProposal` — `{Content, Keywords []string, Tags []string, Description}`. One idea per note. Tags must match the topic taxonomy. Description ≤120 chars.
- `ParseProposal()` — extracts the first balanced JSON object from pi's output

**`version/`** — audit trail:
- `Snapshotter` — copies `pipeline-config.yaml`, AGENTS.md, and skills to `vault/memory/evolved/snapshots/{timestamp}/` before each evolution
- `EvolutionLog` — append-only JSONL at `vault/memory/evolved/evolution-log.jsonl`; each entry: `{Timestamp, SessionID, Score, Summary, ConfigVersion}`

### `internal/skills` — Skills Registry

Skills are on-demand instruction sets loaded from the vault. Each skill directory contains a `SKILL.md` file with YAML frontmatter (name, description, tags) and markdown body (instructions). Additional files in the skill directory are auto-scanned and exposed as `SkillFile` entries (with absolute path, filename, type/extension, and an optional description inherited from frontmatter).

**`Skill`** struct fields: `Name`, `Description`, `Tags []string`, `Instructions` (full SKILL.md body), `Files []SkillFile`, `Dir` (absolute skill directory path).

**`SkillFile`** struct fields: `Name` (filename), `Path` (absolute), `Type` (extension without dot, e.g. `"sh"`, `"ts"`, or `""` for no extension), `Description` (inherited from skill frontmatter manifest).

**`Registry`** — scans skill directories, parses SKILL.md files, auto-discovers sibling files. `Catalog()` returns `[]SkillCatalogEntry` — a compact list (name + description + tags, no instructions or files) for the `SkillAgent`.

**`SkillAgent`** (`agent.go`) — short-lived pi subprocess that selects the best skill for a query. `Find(ctx, query, catalog)` builds a prompt from the catalog, runs a single-turn pi session via `pirun.RunTextSession`, and returns the matched skill name (empty string if none). Short-circuits without spawning pi when catalog is empty.

**`Find_skill` tool** — pi tool (in `extensions/skill-tools.ts`) that triggers skill selection at runtime. When the main pi agent calls `Find_skill`, the Go bridge intercepts the `tool_execution_start` event, invokes `SkillAgent.Find()`, and writes the full `Skill` JSON to `AGENTLOOP_SKILL_LOAD_PATH` for the TS execute() to return to the agent. `Find_skill` events are silently consumed by the bridge — not forwarded to `OnToolUse`.

### `internal/vault` — Session Persistence

**Storage format:** Obsidian-compatible Markdown with YAML frontmatter.

```
~/.local/share/agentloop/vault/
├── sessions/
│   └── YYYY-MM-DD-sess-{uuid8}.md
├── memory/
│   ├── users/    (per-user profiles, YAML)
│   ├── contexts/ (conversation summaries, Markdown + JSON sidecar index)
│   ├── notes/    (per-user SQLite databases for atomic notes + vector search)
│   │   └── {userId}.db
│   ├── cache/    (prompt cache, reserved)
│   └── evolved/  (MemEvolve artifacts)
│       ├── pipeline-config.yaml          (active evolved config)
│       ├── evolution-log.jsonl           (audit trail of all evolutions)
│       ├── metrics/
│       │   └── {userId}-YYYY-MM-DD.jsonl (task outcome records)
│       └── snapshots/
│           └── {timestamp}/              (point-in-time recovery snapshots)
└── skills/       (on-demand skill files)
```

**Session file structure:**
```markdown
---
id: sess-a1b2c3d4
title: First 60 chars of task...
created: 2006-01-02T15:04:05Z07:00
updated: 2006-01-02T15:04:05Z07:00
status: done
provider: anthropic
model: claude-sonnet-4-20250514
source: cli
user_id: marco
tags: []
---

## Task
Original task description

## Tools Used
- bash
- edit

## HITL Log
- 14:00:05 | docker | approve

## Transcript
```
Raw conversation text
```
```

### `internal/security` — Security Policy Validation

**Core validators** (all return `error`, nil = allowed):

| Function | Purpose | Inputs |
|----------|---------|--------|
| `ValidatePath(path, allowedPaths)` | Block path traversal, enforce allowed dirs | `filepath.Clean()` + prefix check |
| `ValidateURL(rawURL, blockedCIDRs, allowedDomains)` | SSRF protection | DNS resolution + CIDR check |
| `ValidateDockerCommand(cmd, allowedSubs, blockedVolPaths)` | Docker restrictions | Subcommand whitelist + volume mount check |

**Prompt injection protection** (`injection.go`):

| Function | Purpose | 
|----------|---------|
| `DetectInjectionRisk(content, source, config)` | Analyze content for prompt injection patterns and assign risk level |
| `ValidateToolCallSource(toolName, filePath, cmd, config)` | Check if tool call from source requires approval |
| `SanitizeContent(content, config)` | Remove/mask sensitive data and injection patterns |

**Injection sources monitored:**
- `SourceSkill` - Skills folder contents  
- `SourceNodeModules` - Node.js dependencies
- `SourceEmailAttachment` - Email attachments
- `SourceCloudFile` - Cloud storage files
- `SourceFetchResponse` - Network fetch responses
- `SourceGitRepo` - Git repository files  
- `SourceUserInput` - Direct user input (safe)

### `internal/config` — Configuration

- `Defaults()` returns production-ready defaults
- `Load(path)` reads YAML, merges env vars, falls back to defaults
- `expandHome()` resolves `~/` to actual home dir for socket path, vault path, log file, skill dirs
- All structs use `mapstructure` tags for Viper compatibility

### `internal/errors` — Error Taxonomy

Four categories:
- `CategoryRetryable` — transient failures, can retry
- `CategoryFatal` — stop immediately
- `CategoryUserAbort` — user cancelled
- `CategoryToolFailure` — tool execution error

Helpers: `Retryable()`, `Fatal()`, `UserAbort()`, `ToolFailure()`, `IsRetryable()`, `IsUserAbort()`

### `internal/logging` — Structured Logging

- Uses `log/slog` (Go stdlib)
- `Init(level, filePath)` sets up logger with stderr + optional file
- Levels: `"debug"`, `"info"`, `"warn"`, `"error"`
- Global `Logger` var, also set as `slog.SetDefault()`

### `internal/pirun` — Shared Pi Session Runner

**`RunTextSession(ctx, piCfg, secCfg, workDir, promptID, prompt)`** — starts a pi subprocess, sends a single prompt, collects the full text response, and returns it. The subprocess exits after responding.

Used by:
- `SkillAgent.Find()` — skill selection side process
- `MetaAgent.Evolve()` — memory evolution side process
- `Orchestrator` — planner/judge single-turn sessions

Only imports `internal/bridge` and `internal/config` — safe to import from any package without circular dependency risk.

---

## TypeScript Extensions Guide

Extensions are TypeScript files in `extensions/` that pi loads via the `-e` flag. They use the pi `ExtensionFactory` pattern.

### Existing Extensions

| Extension | Type | Purpose |
|-----------|------|---------|
| `security-policy.ts` | Permission gate | Blocks dangerous bash patterns, validates file write paths |
| `docker-guard.ts` | Permission gate | Validates docker subcommands + volume mounts |
| `prompt-injection-guard.ts` | Permission gate | Detects prompt injection attempts from risky sources |
| `memory-tools.ts` | Custom tools | Exposes `Add_memory`, `Update_memory`, `Delete_memory`, `Retrieve_memory` tools to pi |
<<<<<<< HEAD
=======
| `skill-tools.ts` | Custom tools | Exposes `Find_skill` tool — LLM-driven skill selection at runtime |
>>>>>>> 236e9d1 (docs(claude): update for LLM-driven skill selection — pirun, SkillAgent, Find_skill tool)

### Extension Environment Variables

| Env Var | Used By | Format | Default |
|---------|---------|--------|---------|
| `AGENTLOOP_ALLOWED_PATHS` | security-policy.ts | Comma-separated paths | (empty = no restriction) |
| `AGENTLOOP_DOCKER_ALLOWED` | docker-guard.ts | Comma-separated subcommands | `ps,logs,images,build,compose,inspect,stats` |
| `AGENTLOOP_DOCKER_BLOCKED_VOLS` | docker-guard.ts | Comma-separated paths | `/etc,/var,/root,/proc,/sys,/dev` |
| `AGENTLOOP_INJECTION_PROTECTION` | **prompt-injection-guard.ts** | **"true"/"false"** | **true** |
| `AGENTLOOP_WHITELIST_SOURCES` | **prompt-injection-guard.ts** | **Comma-separated paths** | **Config whitelist_sources** |
| `AGENTLOOP_BLOCKED_KEYWORDS` | **prompt-injection-guard.ts** | **Comma-separated keywords** | **Config blocked_keywords** |
| `AGENTLOOP_REQUIRE_APPROVAL` | **prompt-injection-guard.ts** | **Comma-separated patterns** | **Config require_approval** |
| `AGENTLOOP_MAX_CONTENT_LENGTH` | **prompt-injection-guard.ts** | **Number** | **50000** |
| `AGENTLOOP_APPROVAL_TIER` | **prompt-injection-guard.ts** | **"owner"/"admin"/"auto-deny"** | **"owner"** |
| `AGENTLOOP_SANITIZE_MEMORY` | **prompt-injection-guard.ts** | **"true"/"false"** | **true** |
<<<<<<< HEAD
| `AGENTLOOP_RETRIEVE_PATH` | memory-tools.ts | Absolute path to temp file | Set by server on session start |
=======
| `AGENTLOOP_RETRIEVE_PATH` | memory-tools.ts | Absolute path to temp file | Set by server per session |
| `AGENTLOOP_SKILL_LOAD_PATH` | skill-tools.ts | Absolute path to temp file | Set by server per session |
>>>>>>> 236e9d1 (docs(claude): update for LLM-driven skill selection — pirun, SkillAgent, Find_skill tool)

### Adding a New Extension

1. Create `extensions/my-extension.ts`
2. Use `ExtensionFactory` pattern with `pi.on("tool_call", ...)` or `pi.addTool({...})`
3. Export default the factory
4. Auto-loaded by PiBridge (picks up all `.ts`/`.js` in extensions dir)
5. For env-based config, use `process.env.AGENTLOOP_*` naming convention

---

## Security Rules

**CRITICAL: Do NOT modify security policies without explicit user approval.**

### Security Boundaries (Two Layers)

**Go layer (subprocess sandbox):**
- `buildSafeEnv()` in `bridge/rpc.go` strips sensitive env vars before pi starts
- `security/policy.go` provides validators

**TypeScript layer (defense in depth):**
- `security-policy.ts` blocks dangerous bash commands + enforces path restrictions
- `docker-guard.ts` blocks unauthorized docker subcommands + volume mounts

### What Counts as a Security Change

Any modification to these requires explicit user approval:
- `internal/security/policy.go` or `policy_test.go`
- **`internal/security/injection.go` or `injection_test.go`** ⭐ **NEW**
- `internal/bridge/rpc.go` (specifically `buildSafeEnv()`)
- `extensions/security-policy.ts`
- `extensions/docker-guard.ts`
- **`extensions/prompt-injection-guard.ts`** ⭐ **NEW**
- `SecurityConfig` fields in `internal/config/config.go` (including `InjectionConfig`)
- Blocked env prefixes, blocked CIDRs, allowed paths, docker rules, **injection config** in `Defaults()`

### Security Test Requirements

After ANY security-related change, these tests MUST pass:
```bash
go test ./internal/security/... -v
go test ./internal/bridge/... -v
```

If adding new security behavior, add corresponding test cases with `t.Fatal("SECURITY: ...")` assertion pattern.

---

## Common Development Tasks

### Adding a New Go Package

1. Create directory: `internal/newpackage/`
2. Create files with `package newpackage` declaration
3. Import using full path: `"github.com/user/agentloop/internal/newpackage"`
4. Run `go build ./...` to verify compilation

### Adding a New Config Field

1. Add field to struct in `internal/config/config.go` (with `mapstructure` tag)
2. Set default in `Defaults()`
3. Add to `configs/agentloop.yaml`
4. If it's a path, add `expandHome()` in `Load()` in `loader.go`
5. Access via `cfg.Section.Field`

### Adding a New JSON-RPC Method

1. Add case in `internal/server/handler.go` `Handle()` switch
2. Parse params, call the appropriate manager/engine method
3. Return result or RPCError
4. If it creates events, use `Broadcaster.Broadcast()` to push notifications

### Adding a New Pi Extension

1. Create `extensions/my-extension.ts`
2. Use `ExtensionFactory` pattern with `pi.on("tool_call", ...)` or `pi.addTool({...})`
3. Export default the factory
4. Auto-loaded on next server start (no Go changes needed)
5. For env-based config, use `process.env.AGENTLOOP_*` naming convention

### Adding a New Skill

1. Create directory in vault skills: `~/.local/share/agentloop/vault/skills/my-skill/`
2. Create `SKILL.md` with YAML frontmatter (name, description, tags) + markdown body
3. Optionally add a `files:` manifest in the frontmatter to provide descriptions for sibling files (`.sh`, `.ts`, etc.)
4. Skills are auto-loaded by the Registry on server start; sibling files are auto-scanned
5. Skills are discovered at runtime via the `Find_skill` tool — the `SkillAgent` selects the best skill based on the agent's natural-language query

### Writing Tests

- Place in `*_test.go` in the same package directory
- Name pattern: `TestFunctionNameBehavior`
- Use `t.TempDir()` for filesystem operations
- For security tests: `t.Fatal("SECURITY: descriptive message")`
- Run relevant tests: `go test ./internal/packagename/... -v`
- Run all tests before finishing: `go test ./...`

### Modifying the RPC Bridge

The bridge (`internal/bridge/rpc.go`) is the most sensitive Go file. When modifying:
- `readEvents()` processes all pi stdout — changes here affect everything
- `buildSafeEnv()` is a security boundary — changes require approval
- `Start()` constructs the pi command — changes affect how pi is launched
- `sendJSON()` is the single write path to pi's stdin — mutex-protected; all sends go through it
- Event type mapping lives in `core.go`'s `SetEventHandler` switch, not in the bridge itself
- Always run bridge tests: `go test ./internal/bridge/... -v`
- To verify the pi protocol, check `$(npm root -g)/@mariozechner/pi-coding-agent/docs/rpc.md`

---

## Vault Session Format

Sessions are stored as Markdown files with YAML frontmatter, compatible with Obsidian.

**Location:** `~/.local/share/agentloop/vault/sessions/`
**Filename pattern:** `YYYY-MM-DD-sess-{uuid8}.md`

**Frontmatter fields:**
- `id` — session ID string (e.g., `sess-a1b2c3d4`)
- `title` — first 60 chars of task description
- `created` / `updated` — RFC3339 timestamps
- `status` — `"starting"`, `"running"`, `"waiting_hitl"`, `"done"`, `"aborted"`, `"error"`
- `provider` — LLM provider name
- `model` — model identifier
- `source` — client source (e.g., `"cli"`, `"slack"`)
- `user_id` — user identifier
- `tags` — string array

**Body sections:** Task, Tools Used, HITL Log, Transcript

When modifying vault behavior:
- Preserve YAML frontmatter format (Obsidian compatibility)
- Keep `renderSession()` in `vault/session.go` as the single rendering function
- `ParseFrontmatter()` splits on `---` delimiters — don't change this protocol

---

## Error Handling Patterns

```go
// Wrap errors with context
return fmt.Errorf("failed to start pi: %w", err)

// Use error categories for routing
return errors.Retryable("connection lost", err)
return errors.Fatal("invalid config", err)
return errors.UserAbort("user cancelled")
return errors.ToolFailure("bash failed", err)

// Check categories
if errors.IsRetryable(err) { /* retry logic */ }
if errors.IsUserAbort(err) { /* clean exit */ }
```

---

## Code Style

- Standard Go formatting (`gofmt`). No custom linter config.
- Error wrapping: `fmt.Errorf("context: %w", err)`
- All config struct fields have `mapstructure` tags matching the YAML key names.
- Exported types/functions have doc comments. Unexported helpers do not require them.
- Prefer returning `error` over panicking.
- Use `slog.Info/Warn/Error/Debug` for logging, not `fmt.Print` (except for user-facing output in the CLI client).
- TypeScript extensions: use `const` for the factory, `export default` at bottom.

### TypeScript Extension Style

- **Event name is `"tool_call"`** — never `"toolCall"`. Pi never fires `"toolCall"`; using it silently registers a dead handler.
- **Event properties**: use `event.toolName` (not `event.tool.name`) and `event.input.<field> as string` (not `event.input?.<field>`).
- **Return values from `tool_call` handlers**:
  - Pass through: `return undefined` — never `return { action: "continue" }`
  - Block: `return { block: true, reason: "..." }` — never `return { action: "block", ... }`
  - HITL: `await ctx.ui.confirm(title, message)` then `return { block: true, reason }` if denied — never `return { action: "request_permission", ... }`
  - Always guard with `if (!ctx.hasUI) return { block: true, reason: "... (no UI)" }` before any `ctx.ui` call.
- **No `for` loops inside handlers** — use `Array.find()` / `Array.some()` / `Array.every()`. Extract named pure functions for all search/match logic so handlers stay declarative:
  ```ts
  // ✅ correct
  function matchedBlockedPattern(cmd: string): string | undefined {
    return blockedPatterns.find((p) => cmd.includes(p));
  }
  const hit = matchedBlockedPattern(command);
  if (hit) return { block: true, reason: `Blocked: "${hit}"` };

  // ❌ wrong
  for (const p of blockedPatterns) {
    if (command.includes(p)) return { block: true, reason: `Blocked: "${p}"` };
  }
  ```
- **No mutable variables inside handlers** — derive all values with `const`; do not reassign with `let`.

---

## Gotchas & Pitfalls

1. **Import paths are absolute** — always `github.com/user/agentloop/internal/...`, never relative.
2. **`min()` is defined locally** in `bridge/rpc.go` and `memory/profile.go` (Go <1.21 compat). If Go version bumps to 1.21+, the built-in `min` will conflict — remove the local ones.
3. **Extensions dir auto-detection** falls back to `{binary-dir}/extensions/`. During development, set `pi.extensions_dir` in config to the absolute path of your `extensions/` directory.
4. **Session IDs use UUID prefixes** — `sess-{uuid8}`. Collision risk is negligible.
5. **`extension_ui_response` is sent as raw JSON** via `sendJSON(ExtensionUIResponse{...})` — NOT wrapped in an `RPCCommand`. Pi expects the response object directly on stdin.
6. **Config YAML keys use snake_case** (`max_concurrent`), matching Go struct `mapstructure` tags.
7. **`ValidateURL()` does real DNS lookups** — tests that call it need network access or should mock.
8. **The `Prompt()` method is non-blocking** — it sends the command and returns immediately. Completion is signaled via `agent_end` event (closes `doneCh`) or pi process exit (closes `b.Done()`).
9. **TypeScript extensions cannot import from each other** — each is loaded independently by pi.
10. **Socket permissions are 0700** (owner-only). The server removes stale sockets on startup.
11. **Memory profile updates use heuristics** — no LLM calls. They extract project paths and topics from user messages.
12. **Compaction is deterministic** — all three strategies (rolling, facts, topics) use string processing, no LLM.
13. **Pi prompt field is `"message"` not `"text"`** — sending `{"type":"prompt","text":"..."}` causes a silent `startsWith` crash in pi's internals. Always use `{"type":"prompt","message":"..."}`.
14. **Pi's "done" signal is `agent_end`** — not a `"done"` type event. The `response` events (e.g. `{"type":"response","command":"prompt","success":true}`) are command acknowledgments, not completion signals. Streaming text comes via `message_update` events with `assistantMessageEvent.type == "text_delta"`.
15. **`BlockedEnvPrefixes` must not block the LLM provider key** — pi needs its provider credentials (e.g. `ANTHROPIC_API_KEY`) to function. The blocked list is for secrets pi's agent should not exfiltrate, not for credentials pi itself uses. If using file-based auth (Claude subscription), this is moot, but keep it in mind for API key setups.
16. **MemEvolve MetaAgent uses a read-only pi session** — the MetaAgent pi subprocess has no write/edit/bash tools. All file changes go through the Go `Applier`. Never give the MetaAgent write access.
17. **AGENTS.md markers are hard boundaries** — `Applier` only writes inside `<!-- EVOLVED:START -->…<!-- EVOLVED:END -->`. Content outside the markers is untouched. If the markers are missing, `Applier` appends them. Do not remove or rename these markers.
18. **Evolved skills are namespaced `evolved-`** — `Applier` enforces this prefix. A proposal with `name: "my-skill"` becomes `evolved-my-skill`. This prevents collisions with hand-authored skills.
19. **Evolution is serialized, not concurrent** — `MetaAgent` holds a mutex for the duration of `Evolve()`. The `Collector` fires triggers as goroutines (`go trigger(outcome)`), so a concurrent trigger will block at the mutex and run after the current evolution finishes. In practice, the `Collector`'s rate limiter (cooldown + daily cap) prevents a burst of queued goroutines. Rate limit counters are in-memory and reset on server restart.
20. **`PipelineHolder.Reload()` is atomic** — it swaps the `atomic.Pointer[Pipeline]` in a single store. In-flight sessions that already called `Get()` continue on the old pipeline for their duration; new `Get()` calls see the new pipeline immediately.
21. **`ExtractKeywords` and `ExtractTopics` are exported from `memory/index.go`** — these were previously unexported. Do not move them; baseline implementations import them by package path.
22. **`NoteStore` interface is test-only** — never accept `NoteStore` as a parameter in production code. Use `*notes.SQLiteNoteStore` directly. `InMemoryNoteStore` exists only for unit tests.
23. **Vector search requires a pre-computed embedding** — `BaselineRetriever` calls `SearchByVector` only when `RetrievalQuery.QueryEmbedding` is non-nil. The retriever does not call any embedding API itself. If `QueryEmbedding` is nil, only keyword search runs; this is correct and expected behavior when no embedding provider is wired.
24. **`Applier.Apply()` requires `userID`** — `NoteProposals` are stored per-user via `Engine.AddNote()`. Passing an empty string silently produces notes with no owner. Always pass `outcome.UserID`. Passing a nil engine silently skips all note proposals.
25. **MemEvolve only creates notes, never updates or deletes them** — `NoteProposal` has no `action` field. Notes written by evolution are immutable knowledge artifacts; the agent accumulates them across evolutions.
26. **Always route note saves through `Engine.AddNote()`** — never call `SQLiteNoteStore.Add()` directly from outside the engine. `Engine.AddNote()` triggers bidirectional auto-linking; bypassing it creates orphaned notes.
27. **`MemScheduler.Schedule()` is deterministic** — caches context at key `"ctx:" + userId + ":" + taskCacheKey(task)`. The cache key is based on normalized task text; different whitespace or casing may produce different keys.
28. **`GetContextForUserWithTask()` vs `GetContextForUser()`** — use `GetContextForUserWithTask` in all new code; `GetContextForUser` is legacy and lacks task-aware depth selection. The task-scoped method goes through the scheduler, pipeline, and note search.
29. **`GetContextForUserAndConversationContext()` for threaded conversations** — when a task is part of a conversation thread (e.g. Slack thread), pass the thread/channel ID as `contextID`. History from other threads is excluded; only that thread's turns are returned.
30. **`PiCompletionClient.Embed()` always returns nil** — pi subprocesses have no embedding API. For vector search to work, you must wire in an external embedding provider. Until then, `QueryEmbedding` will always be nil and only keyword search runs.
31. **`memory.agent.*` config is for delta extraction only** — this is a separate, cheap-model pi subprocess spawned asynchronously per interaction. It is not the main agent. Set `enabled: false` to use `NoopClient` and disable delta extraction entirely.
32. **`Retrieve_memory` tool uses temp-file IPC** — the server writes retrieved notes to the path in `AGENTLOOP_RETRIEVE_PATH` before the session starts; the tool reads synchronously. This temp file is scoped to the session; do not share across sessions.
33. **Memory tool execution stubs in `memory-tools.ts`** — `Add_memory`, `Update_memory`, `Delete_memory` `execute()` functions return success immediately; the Go bridge intercepts `tool_execution_start` events by tool name to perform actual persistence. The TypeScript side only provides the tool schema.
34. **`SQLiteNoteStore.Get(id)` is O(n) in open databases** — it searches all open user DBs. Use user-scoped methods (`SearchByKeywords`, `ListByUser`) where possible; avoid `Get()` in hot paths.
35. **Delta extraction fails silently** — if `LLMClient.Complete()` returns an error or the response lacks the `"DELTA: "` prefix, `RecordInteraction()` continues without saving a delta note. This is intentional; delta extraction is best-effort enrichment.
