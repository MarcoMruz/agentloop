# AgentLoop

An AI agent orchestrator built on top of [pi](https://shittycodingagent.ai) (`@mariozechner/pi-coding-agent`). AgentLoop wraps pi with production-ready guardrails: a human-in-the-loop approval gate, Obsidian-compatible session persistence, security policies that enforce path and network boundaries, and domain-specific tools for web search, Docker, and n8n webhooks.

You run `agentloop run "some task"` and get a supervised, auditable AI agent that asks for your approval before it does anything risky, and saves a full session transcript when it's done.

---

## What It Does

- **Runs tasks through pi** — pi handles the actual agent loop, LLM calls, and coding tools (read, write, edit, bash, grep, find). AgentLoop sits above it, managing the process and extending it.
- **Human-in-the-loop gate** — before docker commands, webhook calls, or any bash command matching risky patterns (`curl`, `git push`, `rm -r`, `sudo`), it stops and asks you to approve or abort.
- **Session persistence** — every run is saved to `~/.local/share/agentloop/vault/sessions/` as a Markdown file with YAML frontmatter, compatible with Obsidian.
- **Security policies** — a sanitized environment is passed to pi (no API keys, tokens, or secrets leak into the subprocess), and TypeScript extensions block dangerous command patterns, enforce filesystem boundaries, and validate Docker subcommands.
- **Domain tools** — adds `web_search` (Brave Search API) and `n8n_webhook` tools to the agent's toolbox via pi extensions.

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  agentloop (Go binary)                               │
│                                                      │
│  CLI (Cobra)                                         │
│   └── Orchestrator          ← task lifecycle         │
│        ├── PiBridge         ← JSON-line RPC to pi    │
│        ├── HITL Gate        ← terminal approval UI   │
│        ├── Vault            ← session persistence     │
│        └── Security         ← path/URL/docker checks │
│                                                      │
└──────────────────┬───────────────────────────────────┘
                   │  JSON over stdin/stdout
                   ▼
┌──────────────────────────────────────────────────────┐
│  pi (Node.js subprocess, --mode rpc)                 │
│                                                      │
│  LLM providers  +  coding tools  +  extensions:      │
│  anthropic          read              hitl-gate.ts    │
│  openai             write             security.ts     │
│  ollama             edit              docker-guard.ts │
│  google...          bash              web-search.ts   │
│                     grep              n8n-webhook.ts  │
└──────────────────────────────────────────────────────┘
```

Go communicates with pi via a JSON line protocol over stdin/stdout. Pi runs in `--mode rpc`, receives a `prompt` command, streams events back (`text`, `tool_use`, `tool_result`, `done`), and exposes an `extension_ui_request` event for HITL approval that routes through the Go process to the user's terminal.

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

# Build the binary
go build -o agentloop ./cmd/agentloop

# Verify it works
./agentloop version
```

To install globally:
```bash
go install ./cmd/agentloop
# Now available as: agentloop
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

### 3. Point extensions to your build (development only)

By default, AgentLoop looks for extensions at `{binary-dir}/extensions/`. If you're running the binary from the project root, this works automatically. If running from elsewhere, set the path explicitly in config:

```yaml
pi:
  extensions_dir: /absolute/path/to/agentloop/extensions
```

---

## Usage

```bash
# Run a task
./agentloop run "add unit tests to the auth package"

# Run with a specific provider
./agentloop --config ~/.config/agentloop/agentloop.yaml run "refactor the database layer"

# Show current resolved config
./agentloop config show

# Print version
./agentloop version
```

### During a task

The agent streams output to your terminal in real time. When it wants to run something risky, it pauses and shows:

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

These are the files to read first when getting familiar with the codebase:

```
agentloop/
│
├── CLAUDE.md                        ← Full dev guidelines for AI agents and humans
├── configs/agentloop.yaml           ← Annotated default config — read this first
├── agents-md/AGENTS.md              ← Instructions the AI agent follows at runtime
│
├── internal/
│   ├── orchestrator/orchestrator.go ← Task lifecycle: how everything connects
│   ├── bridge/rpc.go                ← Core pi integration: subprocess + RPC protocol
│   └── config/config.go             ← All config structs and defaults in one place
│
└── extensions/
    ├── hitl-gate.ts                 ← Where HITL decisions are triggered
    └── security-policy.ts           ← What commands/paths are blocked and why
```

**Recommended reading order:**

1. `configs/agentloop.yaml` — understand what can be configured
2. `internal/config/config.go` — understand the config structs and defaults
3. `internal/orchestrator/orchestrator.go` — understand `RunTask()`, the main execution path
4. `internal/bridge/rpc.go` — understand how Go talks to pi
5. `extensions/hitl-gate.ts` + `extensions/security-policy.ts` — understand the safety layer

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
# Build first
go build -o agentloop ./cmd/agentloop

# Simple task that exercises the full path
./agentloop run "list the files in the current directory and tell me what you see"

# After it completes, verify the session was saved
ls ~/.local/share/agentloop/vault/sessions/

# Check the config show works correctly
./agentloop config show
```

If you've added a new extension, confirm it gets loaded:
- Set `logging.level: debug` in your config
- Run a task — you should see pi debug output for each extension load
- Trigger the extension's condition and verify the expected behavior

---

## Configuration Reference

| Key | Default | Description |
|-----|---------|-------------|
| `vault.path` | `~/.local/share/agentloop/vault` | Where session notes are stored |
| `pi.binary` | `pi` | Path to the pi executable |
| `pi.provider` | `anthropic` | LLM provider (anthropic, openai, ollama, google...) |
| `pi.model` | `claude-sonnet-4-20250514` | Model to use |
| `pi.extensions_dir` | auto-detected | Path to `extensions/` directory |
| `agents.max_iterations` | `25` | Max agent reasoning iterations |
| `agents.max_token_budget` | `200000` | Max tokens per session |
| `hitl.always_pause_tools` | `[docker, n8n_webhook]` | Tools that always require approval |
| `hitl.timeout_seconds` | `300` | Seconds before auto-abort on HITL prompt |
| `security.allowed_paths` | `[~/projects, ~/tmp]` | Paths the agent may read/write |
| `security.blocked_cidrs` | private ranges | IP ranges blocked for outbound requests |
| `logging.level` | `info` | debug, info, warn, error |
| `logging.file` | (stderr only) | Optional log file path |

---

## Optional Tools

### Web Search

Add a `BRAVE_SEARCH_API_KEY` to your environment. The `web_search` tool becomes available to the agent automatically — no config change needed.

```bash
export BRAVE_SEARCH_API_KEY=BSA...
```

Get a free API key at [brave.com/search/api](https://brave.com/search/api/).

### n8n Webhooks

Configure named webhooks in your config:

```yaml
tools:
  n8n:
    webhooks:
      deploy:
        url: https://your-n8n.example.com/webhook/deploy
        auth_header: X-Webhook-Secret
        secret_env_var: N8N_DEPLOY_SECRET
```

Set the secret in your environment:
```bash
export N8N_DEPLOY_SECRET=your-secret
```

The agent can then trigger it with the `n8n_webhook` tool using the name `deploy`.

---

## Contributing

### Adding a new CLI command

1. Create `internal/cli/mycommand.go`
2. Define a `cobra.Command` and its `RunE` handler
3. Register it in `internal/cli/root.go`: `rootCmd.AddCommand(myCmd)`
4. Access config via the package-level `cfg` variable

### Adding a new pi extension (TypeScript)

1. Create `extensions/my-extension.ts`
2. Use the `ExtensionFactory` pattern — see existing extensions for reference
3. Export default the factory function
4. It auto-loads on the next `agentloop run` — no Go changes needed
5. For config, read from `process.env.AGENTLOOP_*` env vars

### Adding a config field

1. Add the field to the right struct in `internal/config/config.go` with a `mapstructure` tag
2. Set its default in `Defaults()`
3. Document it in `configs/agentloop.yaml`

### Security changes

Security policy files require explicit maintainer approval before merging. These are:
- `internal/security/policy.go`
- `internal/bridge/rpc.go` (`buildSafeEnv` function)
- `extensions/security-policy.ts`, `hitl-gate.ts`, `docker-guard.ts`

See `CLAUDE.md` for the full development reference.

---

## Phase 2 (Not Yet Implemented)

These features are stubbed and tracked for a future release:

- **Multi-step plan decomposition** (`internal/orchestrator/plan.go`) — break complex tasks into sub-plans before execution
- **Multi-agent spawning** (`internal/orchestrator/spawn.go`) — run multiple pi instances in parallel on decomposed tasks
- **Session listing** (`agentloop session list`) — browse saved vault sessions from the CLI
- **Vault resume** — reload a previous session's state into pi to continue interrupted work

---

## License

MIT
