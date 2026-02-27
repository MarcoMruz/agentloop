# AgentLoop

**AgentLoop is the single source of truth for all agent intelligence.** It wraps pi (`@mariozechner/pi-coding-agent` v0.54.0) as its underlying coding agent runtime and adds persistent memory, context management, prompt caching, compaction strategies, HITL gating, vault persistence, skill management, and multi-client support via Unix socket API.

Two binaries:
- **`agentloop-server`** — Long-running Go server (Unix socket). Manages sessions, memory, skills, HITL routing, vault persistence.
- **`agentloop`** — Thin CLI client. Connects to the server socket, sends tasks, renders output.

You run `agentloop "some task"` and get a supervised, auditable AI agent that asks for your approval before it does anything risky, saves a full session transcript when done, and learns from interaction patterns across sessions.

---

## What It Does

- **Manages pi subprocesses** — the server spawns one pi instance per session, communicates via JSON-line RPC, manages lifecycle, enforces resource limits.
- **Persistent memory engine** — learns from every interaction: conversation logs, user profiles, communication style, projects, repeated patterns. Uses compaction strategies to keep prompts efficient.
- **Human-in-the-loop gate** — risky operations (docker, path writes, certain bash commands) require your approval before executing. All decisions are logged.
- **Session persistence** — every run is saved to `~/.local/share/agentloop/vault/sessions/` as Obsidian-compatible Markdown with YAML frontmatter. Resumable.
- **Security at two layers** — Go sandbox strips API keys/secrets from pi subprocess env. TypeScript extensions (security-policy.ts, docker-guard.ts) block dangerous patterns, enforce filesystem boundaries.
- **Skill management** — on-demand instructions loaded from vault based on task content. Extensible without recompiling.

---

## Architecture

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  CLI Client  │  │ Slack Bridge │  │ Future: Web  │
│  (Go binary) │  │ (Node.js)    │  │ UI, Mobile   │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       └────────┬────────┘────────┬────────┘
                │  Unix Socket    │
                │  (JSON-RPC)     │
                ▼                 ▼
┌────────────────────────────────────────────────────────────────┐
│  AgentLoop Server (Go, long-running daemon)                    │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │  Agent Core                                              │ │
│  │  • Builds prompts (memory + task + skills)               │ │
│  │  • Manages pi subprocess via RPC                         │ │
│  │  • HITL gate (routes to client via socket)               │ │
│  │  • Stuck detection, resource limits                      │ │
│  │  • Context window management + compaction                │ │
│  └──────┬─────────────────────────────────────────────────┬─┘ │
│         │ stdin/stdout JSON RPC                           │    │
│  ┌──────▼──────────────────────────────────────────────────▼──┐ │
│  │  pi subprocess (per session)                              │ │
│  │  • 15+ LLM providers (anthropic, openai, ollama, etc)     │ │
│  │  • Coding tools: read, write, edit, bash, grep, find      │ │
│  │  • TypeScript extensions: security, docker, hitl gates    │ │
│  └─────────────────────────────────────────────────────────┬─┘ │
│                                                             │   │
│  ┌───────────────────────────────────────────────────────┬─▼──┐ │
│  │  Memory Engine          │  Session Manager            │    │ │
│  │  • User profiles        │  • Lifecycle (start/done)   │    │ │
│  │  • Conversation logs    │  • Concurrency limits       │    │ │
│  │  • Compaction strategy  │  • HITL routing             │    │ │
│  │  • Prompt cache         │  • Abort/steer handling     │    │ │
│  └────────────────────────┴─────────────────────────────────┘ │
│                                                                │
│  ┌────────────────────────────────────────────────────────┐   │
│  │  Vault (~/.local/share/agentloop/vault/)               │   │
│  │  ├── sessions/  (session transcripts)                  │   │
│  │  ├── memory/    (profiles, conversations, cache)       │   │
│  │  └── skills/    (on-demand skill files)                │   │
│  └────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────┘
```

**Data flow for a task:**
1. CLI client sends `task.start` via Unix socket
2. Server creates a session, enforces limits, launches agent goroutine
3. Agent builds prompt (memory prefix + skills + task) and starts pi subprocess
4. Pi processes task, calls tools, sends events back
5. Security extensions validate risky operations; HITL requests route back through socket to client
6. Client shows HITL prompt to user, user approves/denies
7. Server routes approval back to agent, pi continues
8. On completion, server saves to vault and broadcasts `event.done`

---

## Requirements

| Dependency | Version | Install |
|------------|---------|---------|
| Go | >= 1.23.0 | [go.dev/dl](https://go.dev/dl/) |
| Node.js | >= 18 | [nodejs.org](https://nodejs.org/) |
| pi | 0.54.0 | `npm install -g @mariozechner/pi-coding-agent` |

Verify everything is in place:
```bash
go version          # go1.23.0 or newer
pi --version        # 0.54.0
```

---

## Installation

```bash
# Clone the repo
git clone https://github.com/user/agentloop
cd agentloop

# Install Go dependencies
go mod download

# Build both binaries
go build -o agentloop-server ./cmd/agentloop-server
go build -o agentloop ./cmd/agentloop

# Start the server (runs in background)
./agentloop-server &

# Test the connection
./agentloop "list files in the current directory"
```

To install globally:
```bash
go install ./cmd/agentloop-server ./cmd/agentloop
# Now available as: agentloop-server (start in ~/.config/agentloop/start.sh, systemd, etc)
#                   agentloop (use from anywhere)
```

---

## Setup

### 1. Configure your LLM provider

AgentLoop passes provider configuration to pi. Set your API key in your shell environment:

```bash
# For Anthropic (default provider)
export ANTHROPIC_API_KEY=sk-ant-...

# For OpenAI
export OPENAI_API_KEY=sk-...

# For Ollama (local, no key needed)
# Just make sure ollama is running
```

### 2. Create a config file (optional)

Without a config file, AgentLoop runs with sensible defaults. To customise, copy the sample:

```bash
mkdir -p ~/.config/agentloop
cp configs/agentloop.yaml ~/.config/agentloop/agentloop.yaml
```

Key settings to review before your first run:

```yaml
pi:
  provider: anthropic          # or openai, ollama, google...
  model: claude-sonnet-4-20250514

security:
  allowed_paths:               # filesystem paths the agent can write to
    - ~/projects
    - ~/tmp

hitl:
  always_pause_tools:          # tools that always require your approval
    - docker
    - n8n_webhook
  timeout_seconds: 300         # auto-abort if you don't respond in 5 min
```

See the full config reference in [`configs/agentloop.yaml`](configs/agentloop.yaml).

### 3. Extensions (development only)

By default, the server looks for extensions at `{agentloop-server-binary-dir}/extensions/`. If running from the project root, this works automatically. Otherwise, set the path explicitly in config:

```yaml
pi:
  extensions_dir: /absolute/path/to/agentloop/extensions
```

Extensions are TypeScript files (`.ts`) that hook into pi's extension system. They're loaded by the server and run inside each pi subprocess. See `extensions/` directory for examples.

---

## Usage

First, ensure the server is running:
```bash
./agentloop-server &
```

Then use the CLI client:
```bash
# Run a task
./agentloop "add unit tests to the auth package"

# Run with explicit config path
./agentloop "refactor the database layer" --config ~/.config/agentloop/agentloop.yaml

# Check server health
./agentloop --health

# List active sessions (future feature)
./agentloop --sessions
```

### During a task

The agent streams output to your terminal in real time. It processes the task using pi, with your memory context and any loaded skills. When it wants to run something risky, it pauses and shows:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠  HUMAN REVIEW REQUIRED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
AgentLoop HITL: Allow bash command?

Tool: bash
Parameters:
  command:              git push origin main

[a] Approve  [s] Skip  [q] Abort
>
```

Type `a` to approve, `s` to skip that step, or `q` to abort the entire task. Every decision is logged to the session note.

### Session output

After each run, a session note is written to:
```
~/.local/share/agentloop/vault/sessions/YYYY-MM-DD-session-{timestamp}.md
```

This is valid Obsidian-compatible Markdown. Open your vault directory in Obsidian to browse and search sessions.

---

## Project Structure — Start Here

Read these files first when getting familiar with the codebase:

```
agentloop/
│
├── CLAUDE.md                              ← Complete dev guidelines (read this first!)
├── configs/agentloop.yaml                 ← Annotated default config
├── agents-md/AGENTS.md                    ← Instructions pi loads at runtime
│
├── cmd/
│   ├── agentloop-server/main.go           ← Server binary entry point
│   └── agentloop/main.go                  ← CLI client entry point
│
├── internal/
│   ├── server/
│   │   ├── server.go                      ← Unix socket listener, JSON-RPC dispatch
│   │   ├── handler.go                     ← Request handlers (task, session, memory, hitl)
│   │   └── client.go                      ← Connected client state
│   │
│   ├── session/
│   │   ├── manager.go                     ← Session lifecycle, limits, routing
│   │   └── session.go                     ← Session state machine
│   │
│   ├── agent/
│   │   ├── core.go                        ← Agent core: builds prompts, manages pi, HITL
│   │   └── prompt_builder.go              ← Assembles prompt: memory + skills + task
│   │
│   ├── bridge/
│   │   ├── rpc.go                         ← ⭐ Core: pi subprocess RPC client
│   │   ├── events.go                      ← Pi event/command types
│   │   └── rpc_test.go                    ← Tests for env sanitization, RPC protocol
│   │
│   ├── memory/
│   │   ├── engine.go                      ← Memory engine: orchestrates all memory ops
│   │   ├── profile.go                     ← Per-user profile (facts, patterns, prefs)
│   │   ├── conversation.go                ← Conversation logs (per-user, per-day)
│   │   ├── compaction.go                  ← Compaction strategies
│   │   └── cache.go                       ← Prompt cache
│   │
│   ├── skills/
│   │   └── registry.go                    ← Skill registry: loads from vault
│   │
│   ├── vault/
│   │   ├── vault.go                       ← Vault directory management
│   │   ├── session.go                     ← Read/write session markdown
│   │   └── frontmatter.go                 ← YAML frontmatter parser
│   │
│   ├── security/
│   │   ├── policy.go                      ← Path, URL, docker validation
│   │   └── policy_test.go                 ← Security test cases
│   │
│   ├── config/
│   │   ├── config.go                      ← Full config struct + defaults
│   │   └── loader.go                      ← Viper loader
│   │
│   └── ... (hitl, errors, logging packages)
│
└── extensions/
    ├── security-policy.ts                 ← Bash command validation, path enforcement
    └── docker-guard.ts                    ← Docker subcommand + volume validation
```

**Recommended reading order:**

1. `CLAUDE.md` — full architecture guide (15 min)
2. `configs/agentloop.yaml` — what can be configured
3. `internal/config/config.go` — config structs and defaults
4. `internal/server/handler.go` — how JSON-RPC requests are handled
5. `internal/agent/core.go` — agent lifecycle: prompt building, pi management, event loop
6. `internal/bridge/rpc.go` — ⭐ how Go talks to pi subprocess (critical file)
7. `extensions/security-policy.ts` + `docker-guard.ts` — safety layer
8. `internal/memory/engine.go` — how memory context is built and cached

---

## Testing

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/bridge/... -v
go test ./internal/security/... -v

# Security tests — always verify these pass before opening a PR
go test ./internal/security/... -v
go test ./internal/bridge/... -v
```

### Existing test coverage

| Package | Tests |
|---------|-------|
| `internal/bridge` | Env var sanitization, RPC JSON serialization, HITL response format |
| `internal/security` | Path traversal blocked, allowed paths, docker volume mounts, docker subcommands |

### Writing tests for new functionality

Tests live in `*_test.go` in the same package as the code they test. Follow these conventions:

```go
// File: internal/mypackage/thing_test.go
package mypackage

import "testing"

func TestThingBehavior(t *testing.T) {
    // Use t.TempDir() for any filesystem work
    // Use t.Fatal("SECURITY: description") for security-critical assertions
    result, err := DoThing(input)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != expected {
        t.Fatalf("got %v, want %v", result, expected)
    }
}
```

Run only the package you changed during development, then run `go test ./...` before finishing.

### End-to-end testing

To test the full system (requires a configured LLM provider and pi v0.54.0):

```bash
# Build both binaries
go build -o agentloop-server ./cmd/agentloop-server
go build -o agentloop ./cmd/agentloop

# Start the server
./agentloop-server &
SERVER_PID=$!

# Simple task that exercises the full path
./agentloop "list the files in the current directory and tell me what you see"

# After it completes, verify the session was saved
ls ~/.local/share/agentloop/vault/sessions/

# Verify vault structure
ls -la ~/.local/share/agentloop/vault/

# Stop the server
kill $SERVER_PID
```

If you've added a new extension:
- Ensure the extension file is in `extensions/` and has `.ts` or `.js` extension
- Set `logging.level: debug` in `~/.config/agentloop/agentloop.yaml`
- Run the server and a task — you should see extension load events in logs
- Trigger the extension's condition and verify the expected behavior

---

## Configuration Reference

For full config documentation, see `configs/agentloop.yaml`. Below are the main sections:

| Section | Key | Default | Description |
|---------|-----|---------|-------------|
| `server` | `socket_path` | `~/.local/share/agentloop/agentloop.sock` | Unix socket path for client connections |
| `pi` | `binary` | `pi` | Path to pi executable |
| | `provider` | `anthropic` | LLM provider (anthropic, openai, ollama, google, etc.) |
| | `model` | `claude-sonnet-4-20250514` | Model identifier |
| | `extensions_dir` | auto-detected | Path to TypeScript extensions directory |
| | `extra_args` | `[]` | Extra arguments passed to pi |
| `vault` | `path` | `~/.local/share/agentloop/vault` | Root vault directory (sessions, memory, skills) |
| `memory` | `profile_max_entries` | `50` | Max facts per user profile |
| | `conversation_retention_days` | `30` | Keep conversation logs for N days |
| | `cache_ttl_minutes` | `60` | Prompt cache validity period |
| | `compaction_strategy` | `rolling` | rolling, facts, or topics |
| `sessions` | `max_concurrent` | `5` | Max simultaneous sessions |
| | `max_per_user` | `3` | Max concurrent sessions per user |
| | `timeout_minutes` | `60` | Session timeout |
| | `token_budget` | `200000` | Max tokens per session |
| | `max_tool_calls` | `50` | Max tools per session |
| `hitl` | `always_pause_tools` | `[docker]` | Tools requiring manual approval |
| | `timeout_seconds` | `300` | Auto-abort HITL wait after N seconds |
| `security` | `allowed_paths` | `[]` | Whitelisted write paths (empty = no restriction) |
| | `blocked_env_prefixes` | `[AWS_,GITHUB_,OPENAI_]` | Env var prefixes stripped from pi |
| | `blocked_cidrs` | private ranges | IP ranges blocked for outbound |
| `skills` | `directories` | `[vault/skills]` | Directories to scan for skill files |
| `logging` | `level` | `info` | debug, info, warn, error |
| | `file` | (stderr only) | Optional log file path |

---

## Development

### Adding a new JSON-RPC server method

1. Add the case to the switch statement in `internal/server/handler.go`
2. Parse params, call the appropriate manager or engine method
3. Return result or error as JSON
4. If it creates events, use `Broadcaster.Broadcast()` to push to subscribed clients

### Adding a new config field

1. Add field to appropriate struct in `internal/config/config.go` with `mapstructure` tag
2. Set default in `Defaults()`
3. Add to `configs/agentloop.yaml` with documentation
4. If it's a path, call `expandHome()` in `Load()`

### Adding a new pi extension (TypeScript)

1. Create `extensions/my-extension.ts`
2. Use pi's `ExtensionFactory` pattern — see existing extensions for examples
3. Export default the factory function
4. Auto-loaded on server startup (all `.ts` and `.js` files in extensions dir)
5. For env config, read `process.env.AGENTLOOP_*` variables

### Adding a new skill

1. Create directory: `~/.local/share/agentloop/vault/skills/my-skill/`
2. Create `SKILL.md` with YAML frontmatter:
   ```yaml
   ---
   name: My Skill Name
   description: What this skill does
   triggers:
     - keyword1
     - keyword2
   ---
   ```
3. Add markdown instructions below the frontmatter
4. Skills are auto-loaded by `memory/engine.go` — no server restart needed

### Security changes

**CRITICAL:** Security policy changes require explicit approval before merging. These files are protected:
- `internal/security/policy.go` — path/URL/docker validators
- `internal/bridge/rpc.go` — `buildSafeEnv()` function
- `extensions/security-policy.ts` — bash command blocking
- `extensions/docker-guard.ts` — docker restrictions

Run security tests before any PR:
```bash
go test ./internal/security/... -v
go test ./internal/bridge/... -v
```

See `CLAUDE.md` for the complete development guide.

---

## Roadmap

Planned features for future releases:

- **Web UI** — dashboard to view active sessions, memory profiles, vault browser
- **Slack integration** — submit tasks via Slack, receive HITL approvals, session summaries in Slack
- **Session resume** — load a previous session's state and continue interrupted work
- **Multi-agent coordination** — orchestrate multiple agents working on decomposed tasks
- **Advanced memory strategies** — LLM-powered summarization, topic clustering, automated fact extraction
- **Skill marketplace** — share and discover community skills via registries

---

## License

MIT
