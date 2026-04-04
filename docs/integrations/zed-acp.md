# Zed ACP Integration

AgentLoop works as the backend for Zed editor's **Agent Control Protocol (ACP)** — the mechanism Zed uses to delegate AI assistant tasks to an external agent server. This lets you use AgentLoop's full feature set (persistent memory, HITL gating, skill management, session auditing) directly from the Zed editor UI.

---

## Overview

Zed's ACP is a protocol that lets Zed forward AI prompts to an external agent instead of using a built-in LLM provider. When ACP is configured, Zed routes requests from the AI panel to the configured agent server. AgentLoop handles those requests like any other client: it builds a prompt with memory context, runs a pi subprocess, and streams results back.

**Architecture:**

```
Zed editor (AI panel)
    │
    │  ACP protocol (HTTP / stdio)
    ▼
ACP Bridge client (TypeScript, Node.js)
    │
    │  JSON-RPC 2.0 over Unix socket
    ▼
AgentLoop server (~/.local/share/agentloop/agentloop.sock)
    │
    ├─ memory context (user profile + notes + conversation history)
    ├─ skill selection (Find_skill)
    ├─ pi subprocess (LLM + tools)
    └─ session saved to vault (source: zed-acp)
```

Each Zed session that routes through AgentLoop is stored in the vault with `source: zed-acp`, giving you a full audit trail alongside your Slack and CLI sessions.

---

## Features

- **Persistent memory** — AgentLoop remembers your projects, preferences, and past sessions across Zed restarts.
- **HITL gating** — Risky operations (docker, destructive bash, file writes outside allowed paths) pause and ask for approval before executing. Approvals appear in the Zed panel or can be routed to a secondary channel (Slack, CLI).
- **Skill injection** — The `Find_skill` tool automatically loads the best matching skill for your task, augmenting pi's instructions at runtime.
- **Session auditing** — Every task is logged as Obsidian-compatible Markdown in `~/.local/share/agentloop/vault/sessions/`.
- **File context** — The ACP bridge can include open file contents and cursor selections alongside the task prompt, giving the agent richer context without manual copy-paste.
- **Threaded context isolation** — Separate tasks in the same session are tracked independently using Zed's thread/channel IDs. History from one task does not bleed into another.

---

## Prerequisites

- AgentLoop server installed and running (see [Installation](../../README.md#installation))
- Zed editor (version supporting ACP)
- Node.js >= 18
- The AgentLoop ACP bridge client (TypeScript/Node.js)

---

## Setup

### 1. Start the AgentLoop server

```bash
agentloop-server &
```

Verify it is running:
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"health.check","params":{}}' | nc -U ~/.local/share/agentloop/agentloop.sock
```

Expected output: `{"jsonrpc":"2.0","id":1,"result":{"activeSessions":0,"status":"ok"}}`

### 2. Install the ACP bridge client

The bridge client is a Node.js process that sits between Zed and the AgentLoop Unix socket. It translates Zed's ACP protocol into AgentLoop's JSON-RPC format and streams responses back.

```bash
# Clone the bridge client repository
git clone https://github.com/user/agentloop-acp-bridge
cd agentloop-acp-bridge

# Install dependencies
npm install

# Build
npm run build
```

### 3. Configure the bridge

Create a config file at `~/.config/agentloop-acp/config.json` (or set env vars):

```json
{
  "socketPath": "~/.local/share/agentloop/agentloop.sock",
  "userId": "your-username",
  "defaultWorkDir": "~/projects",
  "hitlChannel": "zed"
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `socketPath` | `~/.local/share/agentloop/agentloop.sock` | Path to AgentLoop Unix socket |
| `userId` | system username | User identifier for memory and session tracking |
| `defaultWorkDir` | current directory | Working directory passed to the agent |
| `hitlChannel` | `zed` | Where HITL approval prompts are shown (`zed`, `slack`, `cli`) |

### 4. Register as a Zed ACP provider

In Zed's settings (`~/.config/zed/settings.json`), add:

```json
{
  "agent": {
    "provider": {
      "name": "agentloop",
      "type": "acp",
      "endpoint": "http://localhost:7842"
    }
  }
}
```

The bridge client listens on port `7842` by default (configurable). Zed discovers the agent via this endpoint.

Start the bridge:
```bash
node dist/index.js
# or, to keep it running
pm2 start dist/index.js --name agentloop-acp
```

---

## Configuration

The AgentLoop server does not require special configuration for Zed ACP. All tasks arrive via the standard Unix socket and are handled identically to CLI or Slack tasks. The `source` field is set to `"zed-acp"` by the bridge, which appears in the vault session frontmatter.

Relevant server config sections (in `~/.config/agentloop/agentloop.yaml`):

```yaml
sessions:
  max_per_user: 5          # max concurrent Zed sessions per user
  timeout_minutes: 30      # session auto-timeout

hitl:
  always_pause_tools:      # tools that pause for approval in every client
    - docker
  timeout_seconds: 300     # how long to wait for HITL approval before denying

security:
  allowed_paths:           # file paths the agent may write to
    - ~/projects
    - ~/tmp
```

### File context

The bridge can send structured file context alongside the task prompt. This is controlled by the bridge config:

```json
{
  "fileContext": {
    "includeOpenFiles": true,
    "includeSelection": true,
    "maxFileBytes": 102400
  }
}
```

When enabled, the bridge attaches the currently open file contents and any selected text to the task. The agent receives this as part of the prompt; no special server-side configuration is needed.

---

## Usage

With the server and bridge running, and Zed configured to use AgentLoop:

1. Open the **AI panel** in Zed (`Cmd/Ctrl + ?` or via the AI menu)
2. Type your task in the input box
3. The task is sent to AgentLoop via the bridge
4. Streaming output appears in the Zed AI panel in real time
5. If the agent needs approval for a risky operation, a HITL prompt appears in the panel — click **Approve** or **Deny**

### What each session looks like

- **In Zed**: streaming text in the AI panel, HITL approval dialogs when needed
- **In the vault**: `~/.local/share/agentloop/vault/sessions/YYYY-MM-DD-sess-{id}.md` with `source: zed-acp`
- **In memory**: the interaction is stored and influences future sessions (user profile, notes, conversation history)

### Checking past Zed sessions

```bash
# List all Zed ACP sessions
grep -l 'source: zed-acp' ~/.local/share/agentloop/vault/sessions/*.md

# View a specific session
cat ~/.local/share/agentloop/vault/sessions/YYYY-MM-DD-sess-{id}.md
```

Or open `~/.local/share/agentloop/vault/` as an Obsidian vault to browse sessions with full-text search.

---

## Session flow

```
Zed AI panel → ACP request → Bridge client
    │
    │  task.start { userId, text, workDir, source: "zed-acp", channel_id, thread_id }
    ▼
AgentLoop server
    │
    ├─ Loads memory context for userId
    ├─ Selects skills matching the task
    ├─ Spawns pi subprocess with prompt
    │
    │  Streaming events back to bridge:
    │  event.text       → AI panel text chunk
    │  event.tool_use   → "Using bash..."
    │  event.hitl_req   → approval dialog in Zed
    │  event.done       → session complete
    ▼
Bridge client → ACP response stream → Zed AI panel
```

### Thread isolation

If your Zed integration provides a thread or conversation ID (e.g., an editor tab ID), pass **both** `channel_id` and `thread_id` in `task.start`. AgentLoop constructs the conversation context key as `channelID:threadID` — both fields must be non-empty for thread isolation to activate. When either is missing, sessions fall back to the global conversation history for that user. This keeps context accurate when you switch between files or tasks in Zed.

---

## HITL in Zed

When the agent encounters a risky operation, it pauses and sends a `event.hitl_request` notification. The bridge relays this to Zed, which displays an approval dialog:

```
┌─────────────────────────────────────────┐
│ ⚠  Approval required                    │
│                                         │
│ Tool: bash                              │
│ Command: git push origin main           │
│                                         │
│  [Approve]  [Skip]  [Abort]             │
└─────────────────────────────────────────┘
```

The bridge sends the user's decision back to the server via `hitl.respond`. All decisions are logged to the session transcript.

If you do not respond within `hitl.timeout_seconds` (default: 300 s), the server automatically applies `hitl.timeout_action` (default: `deny`).

---

## Troubleshooting

**Bridge cannot connect to socket**

```
Error: connect ENOENT ~/.local/share/agentloop/agentloop.sock
```

The server is not running. Start it: `agentloop-server &`

**Tasks hang without output**

Check the server log (`~/.config/agentloop/agentloop.log`) for errors. Common causes:
- LLM provider API key not set
- `pi` binary not found (`pi --version` should work)
- Extension load errors (set `logging.level: debug` to see detail)

**HITL dialogs do not appear in Zed**

Verify `hitlChannel: "zed"` in the bridge config. If using a different channel (`slack`, `cli`), approvals will go there instead.

**Session `source` shows as empty**

The bridge must pass `"source": "zed-acp"` in `task.start`. If missing, sessions will have an empty source field and memory attribution may be incorrect.

---

## Client migration note

The original Zed ACP bridge was implemented in Rust (`agentloop-rs`, `agentloop-bridge` crate). The current implementation is TypeScript/Node.js to align with the rest of the AgentLoop client ecosystem. The server-side protocol is unchanged — the Unix socket JSON-RPC API is the same regardless of which client connects.

If you have an existing Rust bridge installation, the TypeScript bridge is a drop-in replacement: point it at the same socket path and use the same `userId` to continue from the same memory state.
