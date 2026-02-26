# AgentLoop — Development Guidelines

## What Is This Project

AgentLoop is a Go CLI that orchestrates **pi** (`@mariozechner/pi-coding-agent` v0.54.0) as its underlying agent runtime. Instead of reimplementing an agent loop, AgentLoop leverages pi's infrastructure and extends it with HITL safety gates, Obsidian-compatible session persistence, security policies, and domain-specific tools.

**Two layers:**
- **Go layer** — CLI, orchestrator, vault, config, pi process management, security validation
- **TypeScript layer** — pi extensions that add HITL gating, security enforcement, and tools (web search, n8n, docker guard)

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  agentloop CLI (Go binary)                              │
│                                                         │
│  cmd/agentloop/main.go                                  │
│       │                                                 │
│       ▼                                                 │
│  internal/cli/          Cobra commands (run, session,   │
│       │                 config, version)                │
│       ▼                                                 │
│  internal/orchestrator/ RunTask() — session lifecycle   │
│       │                                                 │
│       ├──► internal/hitl/       HITL terminal prompts   │
│       ├──► internal/vault/      Session persistence     │
│       ├──► internal/config/     Viper-based config      │
│       ├──► internal/security/   Path/URL/Docker checks  │
│       ├──► internal/errors/     Categorized errors      │
│       └──► internal/logging/    slog wrapper            │
│       │                                                 │
│       ▼                                                 │
│  internal/bridge/       Pi RPC Bridge (stdin/stdout)    │
│       │                                                 │
└───────┼─────────────────────────────────────────────────┘
        │ JSON-line RPC protocol
        ▼
┌─────────────────────────────────────────────────────────┐
│  pi coding agent (Node.js subprocess, --mode rpc)       │
│                                                         │
│  15+ LLM providers ─── Built-in tools ─── Extensions:  │
│  (anthropic, openai,   (read, write,      hitl-gate.ts  │
│   ollama, google...)    edit, bash,        security.ts   │
│                         grep, find, ls)    web-search.ts │
│                                            n8n-webhook.ts│
│                                            docker-guard  │
└─────────────────────────────────────────────────────────┘
```

### Data Flow for a Task

```
User runs: agentloop run "fix the login bug"
    │
    ▼
cli/run.go ──► orchestrator.RunTask(ctx, task, workDir)
    │
    ├─ 1. Create SessionNote (id=session-{unix}, status=in_progress)
    ├─ 2. Create PiBridge, register event + HITL handlers
    ├─ 3. bridge.Start() launches: pi --mode rpc --provider X --model Y -e ext1.ts -e ext2.ts
    │      └─ env filtered via buildSafeEnv() (strips ANTHROPIC_*, OPENAI_*, etc.)
    ├─ 4. bridge.Prompt() sends JSON: {"type":"prompt","id":"...","text":"fix the login bug"}
    ├─ 5. Event loop:
    │      text events        → printed to terminal + collected in transcript
    │      tool_use events    → tool name tracked
    │      tool_result events → logged
    │      extension_ui_request → HITL handler → AskUser() → response back to pi
    │      error events       → logged + printed
    │      done event         → loop exits
    ├─ 6. Wait on <-bridge.Done() or <-ctx.Done() (Ctrl+C)
    └─ 7. Save SessionNote to vault as Markdown with YAML frontmatter
```

### HITL Approval Flow (Cross-Layer)

```
Pi extension (hitl-gate.ts) detects risky tool call
    │
    ▼
ctx.ui.confirm("Allow docker?", "$ docker run ...")
    │
    ▼
Pi sends: {"type":"extension_ui_request","id":"req-1","method":"confirm","title":"..."}
    │
    ▼
Go bridge.readEvents() receives event, calls onHITL handler
    │
    ▼
hitl.AskUser() prints prompt to terminal, blocks on stdin
    │
    ▼
User types: a (approve) / s (skip) / q (abort)
    │
    ▼
Go sends back: {"type":"extension_ui_response","id":"req-1","confirmed":true}
    │
    ▼
Pi extension receives approval, tool executes (or is blocked)
```

---

## Directory Structure

```
agentloop/
├── cmd/agentloop/main.go           # Entry point, calls cli.Execute(version)
├── internal/
│   ├── cli/
│   │   ├── root.go                 # Cobra root cmd, initConfig(), version cmd
│   │   ├── run.go                  # "agentloop run" — task execution
│   │   ├── session.go              # "agentloop session list" (Phase 2 stub)
│   │   └── config.go               # "agentloop config show"
│   ├── orchestrator/
│   │   ├── orchestrator.go         # RunTask() — main session lifecycle
│   │   ├── plan.go                 # Phase 2 stub: plan decomposition
│   │   └── spawn.go                # Phase 2 stub: multi-agent spawning
│   ├── bridge/
│   │   ├── events.go               # RPCCommand, RPCEvent, ExtensionUIResponse types
│   │   ├── rpc.go                  # PiBridge: subprocess management, event routing
│   │   └── rpc_test.go             # Tests: env filtering, serialization
│   ├── hitl/
│   │   ├── gate.go                 # Decision type, FormatSummary()
│   │   └── prompt.go               # AskUser() — terminal input with timeout
│   ├── vault/
│   │   ├── vault.go                # Vault dir creation, SessionsDir()
│   │   ├── session.go              # SessionNote, Write(), Read(), renderSession()
│   │   └── frontmatter.go          # SessionFrontmatter type, ParseFrontmatter()
│   ├── security/
│   │   ├── policy.go               # ValidatePath, ValidateURL, ValidateDockerCommand
│   │   └── policy_test.go          # Security assertion tests
│   ├── logging/
│   │   └── logging.go              # slog Init(), Logger var
│   ├── errors/
│   │   └── errors.go               # AgentError, Category, helpers
│   └── config/
│       ├── config.go               # All config structs, Defaults()
│       └── loader.go               # Load(), DefaultConfigPath(), expandHome()
├── extensions/
│   ├── security-policy.ts          # Blocks dangerous bash patterns + path validation
│   ├── hitl-gate.ts                # HITL approval for risky tools/commands
│   ├── web-search.ts               # Brave Search API tool
│   ├── n8n-webhook.ts              # n8n webhook trigger tool
│   └── docker-guard.ts             # Docker subcommand + volume mount restrictions
├── agents-md/
│   └── AGENTS.md                   # Instructions pi loads for agent behavior
├── configs/
│   └── agentloop.yaml              # Default/sample config file
├── go.mod                          # Module: github.com/user/agentloop
└── go.sum
```

---

## Prerequisites

- **Go** >= 1.23.0
- **pi** v0.54.0 installed globally: `npm install -g @mariozechner/pi-coding-agent`
- Verify: `pi --version` should output `0.54.0`

---

## Build & Run

```bash
# Build
go build -o agentloop ./cmd/agentloop

# Build with version tag
go build -ldflags="-X main.version=v1.0.0" -o agentloop ./cmd/agentloop

# Run commands
./agentloop version
./agentloop config show
./agentloop run "describe the task here"

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

# Security tests MUST always pass — these are mandatory
go test ./internal/security/... -v
go test ./internal/bridge/... -v
```

### Existing Tests

| Test | File | Verifies |
|------|------|----------|
| `TestBuildSafeEnv` | `internal/bridge/rpc_test.go` | API keys stripped from pi subprocess env |
| `TestRPCCommandSerialization` | `internal/bridge/rpc_test.go` | JSON protocol correctness |
| `TestExtensionUIResponseSerialization` | `internal/bridge/rpc_test.go` | HITL response format |
| `TestValidatePathTraversal` | `internal/security/policy_test.go` | `../../etc/passwd` blocked |
| `TestValidatePathAllowed` | `internal/security/policy_test.go` | Valid paths permitted |
| `TestValidateDockerBlocked` | `internal/security/policy_test.go` | Dangerous volume mounts rejected |
| `TestValidateDockerSubcommand` | `internal/security/policy_test.go` | Disallowed subcommands rejected |

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
import "github.com/user/agentloop/internal/bridge"
import "github.com/user/agentloop/internal/config"
import "github.com/user/agentloop/internal/errors"
import "github.com/user/agentloop/internal/hitl"
import "github.com/user/agentloop/internal/logging"
import "github.com/user/agentloop/internal/orchestrator"
import "github.com/user/agentloop/internal/security"
import "github.com/user/agentloop/internal/vault"
```

**Direct dependencies (go.mod):**
```
github.com/spf13/cobra   v1.10.2   # CLI framework
github.com/spf13/viper   v1.21.0   # Config file parsing + env vars
gopkg.in/yaml.v3         v3.0.1    # YAML marshal/unmarshal
```

Do NOT add dependencies without explicit approval. The project intentionally has a minimal dependency footprint.

---

## Configuration System

**Default config path:** `~/.config/agentloop/agentloop.yaml`
**Sample config:** `configs/agentloop.yaml`
**Override via CLI:** `--config /path/to/config.yaml`
**Override via env:** `AGENTLOOP_*` prefix (Viper auto-binding)

### Config Loading Order
1. File at `--config` flag path (or default path)
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
| `vault` | `VaultConfig` | Vault storage path, auto-open setting |
| `pi` | `PiConfig` | Pi binary path, provider, model, extensions dir, extra args |
| `agents` | `AgentConfig` | Max iterations, stuck threshold, token budget, tool call limit |
| `hitl` | `HITLConfig` | Confidence threshold, always-pause tools, timeout |
| `security` | `SecurityConfig` | Allowed paths, blocked env prefixes, blocked CIDRs, docker rules |
| `tools` | `ToolsConfig` | Web search provider/limits, n8n webhook configs |
| `logging` | `LoggingConfig` | Log level, log file path |

---

## Package-by-Package Guide

### `internal/bridge` — Pi RPC Bridge (CORE)

This is the most critical package. It manages the pi subprocess and all communication.

**Key type:** `PiBridge` — manages pi as a child process over JSON-line RPC.

**Lifecycle:**
```
New(piCfg, secCfg) → SetEventHandler() → SetHITLHandler() → Start(ctx, workDir) → Prompt() → <-Done() → Stop()
```

**RPC Protocol (JSON lines over stdin/stdout):**

Commands (Go → pi via stdin):
```json
{"type":"prompt","id":"session-123","text":"task description"}
{"type":"steer","id":"session-123","text":"change direction"}
{"type":"abort","id":"session-123"}
{"type":"extension_ui_response","id":"req-1","text":"{...}"}
```

Events (pi → Go via stdout):
```json
{"type":"text","content":"I'll start by..."}
{"type":"tool_use","name":"bash","input":{"command":"ls -la"}}
{"type":"tool_result","name":"bash","output":"total 42\n..."}
{"type":"done","id":"session-123"}
{"type":"error","message":"something failed"}
{"type":"extension_ui_request","id":"req-1","method":"confirm","title":"Allow docker?"}
```

**Important implementation details:**
- Scanner buffer is 10MB max line (for large tool outputs)
- `buildSafeEnv()` strips env vars matching `BlockedEnvPrefixes` before subprocess creation
- Extensions auto-loaded from `ExtensionsDir` (all `.ts`/`.js` files)
- If `ExtensionsDir` is empty, auto-detects `{binary-dir}/extensions/`
- `SendCommand()` is mutex-protected for thread safety

### `internal/orchestrator` — Task Execution

**Key method:** `RunTask(ctx, taskDescription, workDir) error`

This is the main entry point for task execution. It:
1. Creates a session ID (`session-{unix-timestamp}`)
2. Wires up PiBridge with event + HITL handlers
3. Starts pi, sends the prompt, waits for completion
4. Saves session to vault on completion

**Phase 2 stubs:** `plan.go` and `spawn.go` are empty placeholders for multi-step plan decomposition and multi-agent spawning. Do not add functionality to these unless implementing Phase 2 features.

### `internal/hitl` — Human-in-the-Loop

**`AskUser(toolName, args, title, timeoutSec) (Decision, error)`**
- Prints formatted summary to terminal
- Blocks on stdin: `a`/`approve`/`yes`/`y` → Approve, `s`/`skip` → Skip, anything else → Abort
- Timeout defaults to 300s, returns Abort on timeout
- Decisions are logged in the vault session note

### `internal/vault` — Session Persistence

**Storage format:** Obsidian-compatible Markdown with YAML frontmatter.

```
~/.local/share/agentloop/vault/
├── sessions/
│   └── YYYY-MM-DD-session-{timestamp}.md
└── memory/           # Created but unused (Phase 2)
```

**Session file structure:**
```markdown
---
id: session-1708995234
title: First 60 chars of task...
created: 2006-01-02T15:04:05Z07:00
updated: 2006-01-02T15:04:05Z07:00
status: done
provider: anthropic
model: claude-sonnet-4-20250514
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

Three validators, all return `error` (nil = allowed):

| Function | Purpose | Inputs |
|----------|---------|--------|
| `ValidatePath(path, allowedPaths)` | Block path traversal, enforce allowed dirs | `filepath.Clean()` + prefix check |
| `ValidateURL(rawURL, blockedCIDRs, allowedDomains)` | SSRF protection | DNS resolution + CIDR check |
| `ValidateDockerCommand(cmd, allowedSubs, blockedVolPaths)` | Docker restrictions | Subcommand whitelist + volume mount check |

### `internal/config` — Configuration

- `Defaults()` returns production-ready defaults
- `Load(path)` reads YAML, merges env vars, falls back to defaults
- `expandHome()` resolves `~/` to actual home dir
- All structs use `mapstructure` tags for Viper compatibility

### `internal/errors` — Error Taxonomy

Four categories for orchestrator routing:
- `CategoryRetryable` — transient failures, can retry
- `CategoryFatal` — stop immediately
- `CategoryUserAbort` — user cancelled (Ctrl+C / HITL abort)
- `CategoryToolFailure` — tool execution error

Helpers: `Retryable()`, `Fatal()`, `UserAbort()`, `ToolFailure()`, `IsRetryable()`, `IsUserAbort()`

### `internal/logging` — Structured Logging

- Uses `log/slog` (Go stdlib)
- `Init(level, filePath)` sets up logger with stderr + optional file
- Levels: `"debug"`, `"info"`, `"warn"`, `"error"`
- Global `Logger` var, also set as `slog.SetDefault()`

### `internal/cli` — CLI Commands

| Command | File | Description |
|---------|------|-------------|
| `agentloop run [task]` | `run.go` | Execute a task via orchestrator |
| `agentloop config show` | `config.go` | Print current merged config |
| `agentloop session list` | `session.go` | Phase 2 stub |
| `agentloop version` | `root.go` | Print version string |

**Adding a new CLI command:**
1. Create `internal/cli/newcommand.go`
2. Define `var newCmd = &cobra.Command{...}`
3. Add to root in `root.go`: `rootCmd.AddCommand(newCmd)`
4. Access config via the package-level `cfg` variable

---

## TypeScript Extensions Guide

Extensions are TypeScript files in `extensions/` that pi loads via the `-e` flag. They use the pi `ExtensionFactory` pattern.

### Extension Pattern

```typescript
import type { ExtensionFactory } from "@mariozechner/pi-coding-agent";

const myExtension: ExtensionFactory = (pi) => {
  // Intercept tool calls (permission gate)
  pi.on("toolCall", async (event, ctx) => {
    if (event.tool.name !== "bash") return { action: "continue" };
    // ... validation logic ...
    return { action: "block", message: "Reason" };
    // or
    return { action: "continue" };
  });

  // Add a new tool
  pi.addTool({
    name: "my_tool",
    description: "What it does",
    parameters: {
      type: "object",
      properties: {
        param1: { type: "string", description: "..." },
      },
      required: ["param1"],
    },
    async execute(input) {
      // ... implementation ...
      return { output: "result" };
      // or
      return { error: "error message" };
    },
  });
};

export default myExtension;
```

### Existing Extensions

| Extension | Type | Purpose |
|-----------|------|---------|
| `security-policy.ts` | Permission gate | Blocks dangerous bash patterns, validates file write paths |
| `hitl-gate.ts` | Permission gate | Triggers HITL approval for risky tools/commands |
| `docker-guard.ts` | Permission gate | Validates docker subcommands + volume mounts |
| `web-search.ts` | Tool | Brave Search API (`web_search` tool) |
| `n8n-webhook.ts` | Tool | n8n webhook trigger (`n8n_webhook` tool) |

### Extension Environment Variables

Extensions read config from env vars set by the Go orchestrator or the user's shell:

| Env Var | Used By | Format | Default |
|---------|---------|--------|---------|
| `AGENTLOOP_ALLOWED_PATHS` | security-policy.ts | Comma-separated paths | (empty = no restriction) |
| `AGENTLOOP_HITL_TOOLS` | hitl-gate.ts | Comma-separated tool names | `docker,n8n_webhook` |
| `AGENTLOOP_DOCKER_ALLOWED` | docker-guard.ts | Comma-separated subcommands | `ps,logs,images,build,compose,inspect,stats` |
| `AGENTLOOP_DOCKER_BLOCKED_VOLS` | docker-guard.ts | Comma-separated paths | `/etc,/var,/root,/proc,/sys,/dev` |
| `AGENTLOOP_N8N_WEBHOOKS` | n8n-webhook.ts | `name:url:header:envvar,...` | (empty = tool not registered) |
| `BRAVE_SEARCH_API_KEY` | web-search.ts | API key string | (required for web_search) |

### Adding a New Extension

1. Create `extensions/my-extension.ts`
2. Follow the `ExtensionFactory` pattern above
3. Export default the factory function
4. It will be auto-loaded by `PiBridge.Start()` (picks up all `.ts`/`.js` in extensions dir)
5. If it needs config, read from `process.env.AGENTLOOP_*` variables
6. If adding a new env var, document it in this section

---

## Security Rules

**CRITICAL: Do NOT modify security policies without explicit user approval.**

### Security Boundaries (Two Layers)

**Go layer (subprocess sandbox):**
- `buildSafeEnv()` in `bridge/rpc.go` strips sensitive env vars before pi starts
- `security/policy.go` provides validators used by orchestrator

**TypeScript layer (defense in depth):**
- `security-policy.ts` blocks dangerous bash commands + enforces path restrictions
- `docker-guard.ts` blocks unauthorized docker subcommands + volume mounts
- `hitl-gate.ts` requires human approval for risky operations

### What Counts as a Security Change

Any modification to these requires explicit user approval:
- `internal/security/policy.go` or `policy_test.go`
- `internal/bridge/rpc.go` (specifically `buildSafeEnv()`)
- `extensions/security-policy.ts`
- `extensions/docker-guard.ts`
- `extensions/hitl-gate.ts`
- `SecurityConfig` fields in `internal/config/config.go`
- Blocked env prefixes, blocked CIDRs, allowed paths, docker rules in `Defaults()`

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

### Adding a New CLI Command

1. Create `internal/cli/mycommand.go`
2. Define the cobra command and its `RunE` function
3. Register in `internal/cli/root.go` init(): `rootCmd.AddCommand(myCmd)`
4. Use the package-level `cfg` for config access

### Adding a New Pi Extension

1. Create `extensions/my-extension.ts`
2. Use `ExtensionFactory` pattern with `pi.on("toolCall", ...)` or `pi.addTool({...})`
3. Export default the factory
4. Auto-loaded on next `agentloop run` (no Go changes needed)
5. For env-based config, use `process.env.AGENTLOOP_*` naming convention

### Adding a New Pi Tool (via Extension)

1. Create or edit an extension in `extensions/`
2. Use `pi.addTool({ name, description, parameters, execute })`
3. Parameters follow JSON Schema format (`type: "object"`, `properties`, `required`)
4. `execute(input)` must return `{ output: string }` or `{ error: string }`
5. If tool needs HITL gating, add its name to `AGENTLOOP_HITL_TOOLS` or `hitl.always_pause_tools` config

### Implementing Phase 2 Features

Phase 2 stubs exist at:
- `internal/orchestrator/plan.go` — Multi-step plan decomposition
- `internal/orchestrator/spawn.go` — Multi-agent spawning
- `internal/cli/session.go` — Session listing from vault

When implementing these:
- Remove the stub comment, keep `package orchestrator`
- Follow existing patterns in `orchestrator.go` for how to use bridge/vault/config
- `session list` should read from `vault.SessionsDir()` and display frontmatter
- Add tests for new functionality

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
- Always run bridge tests: `go test ./internal/bridge/... -v`
- Test with actual pi if possible: `./agentloop run "list files in current directory"`

---

## Vault Session Format

Sessions are stored as Markdown files with YAML frontmatter, compatible with Obsidian.

**Location:** `~/.local/share/agentloop/vault/sessions/`
**Filename pattern:** `YYYY-MM-DD-session-{unix-timestamp}.md`

**Frontmatter fields:**
- `id` — session ID string
- `title` — first 60 chars of task description
- `created` / `updated` — RFC3339 timestamps
- `status` — `"in_progress"` or `"done"`
- `provider` — LLM provider name
- `model` — model identifier
- `tags` — string array (currently unused)

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

// Use error categories for orchestrator routing
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
- Use `slog.Info/Warn/Error/Debug` for logging, not `fmt.Print` (except for user-facing terminal output in orchestrator and HITL).
- TypeScript extensions: use `const` for the factory, `export default` at bottom.

---

## Gotchas & Pitfalls

1. **Import paths are absolute** — always `github.com/user/agentloop/internal/...`, never relative.
2. **`min()` is defined locally** in `bridge/rpc.go` (Go <1.21 compat in the file). If Go version bumps to 1.21+, the built-in `min` will conflict — remove the local one.
3. **Extensions dir auto-detection** falls back to `{binary-dir}/extensions/`. During development, set `pi.extensions_dir` in config to the absolute path of your `extensions/` directory.
4. **Vault `memory/` directory** is created but unused. It's reserved for Phase 2.
5. **Session IDs use unix timestamps** — not UUIDs. Two tasks started in the same second would collide.
6. **`extension_ui_response`** is sent as an RPC command with the JSON response embedded in the `Text` field. This is a bridge protocol detail, not a pi standard.
7. **Config YAML keys use snake_case** (`max_iterations`), but Go struct tags use `mapstructure:"snake_case"`. The `yaml.Marshal` output uses Go field names lowercased — this is cosmetic only; Viper reads them correctly.
8. **`ValidateURL()` does real DNS lookups** — tests that call it need network access or should mock.
9. **The `Prompt()` method is non-blocking** — it sends the command and returns immediately. Completion is signaled via the `Done()` channel.
10. **TypeScript extensions cannot import from each other** — each is loaded independently by pi.
