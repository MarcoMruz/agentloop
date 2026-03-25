# LLM-Driven Skill Selection — Design Spec

**Date:** 2026-03-25
**Branch:** feat/memory-architecture-refactor

---

## Problem

Skills are currently selected at prompt-build time via lexical keyword matching (`DetectSkills()`). This approach:
- Burdens skill authors to write triggers that match the heuristic's expectations
- Silently hides skills when trigger words don't match the task text
- Fails for skills copied from external sources with different wording conventions
- Injects all matched skill instructions into the initial prompt regardless of relevance

## Goal

Let the main pi agent discover and load the right skill autonomously at runtime via a single tool call. A small side agent handles the search and selection entirely within its own context window. The main agent's context stays compact — it sends a natural-language request and gets back only the chosen skill's full details.

---

## Architecture

### Overview

One new pi tool (`Find_skill`) exposed via a TypeScript extension (`skill-tools.ts`). When the main agent calls it, the Go side spins up a lightweight `SkillAgent` — a short-lived pi subprocess that reasons over the full skill catalog and returns the best match. The main agent gets back only the result (skill instructions + files). No catalog browsing, no pagination, no intermediate context overhead.

Pattern: `MetaAgent.runPiSession()` in `internal/memory/evolve/meta/agent.go` — a bridge-based pi subprocess that receives a prompt, streams a response, and exits.

### Agent Interaction Pattern

```
Main agent: Find_skill(query: "I need to deploy a Docker container to production")
    │
    ▼
Go side: SkillAgent.Find(query, catalogSnapshot)
    │  (side pi subprocess — own context window)
    │  Receives: full skill catalog (name + description + tags for all skills)
    │  Reasons over it, returns: skill name or "none"
    ▼
Go side: registry.Get(selectedName) → marshal full Skill to temp file
    │
    ▼
TS execute(): reads temp file → returns {name, description, instructions, dir, files} to main agent
```

The main agent's context receives only the final skill result — a single tool call / tool result pair.

---

## Component Changes

### 1. Config — `skills` Section (`internal/config/config.go` + `configs/agentloop.yaml`)

Add `SkillAgentConfig` nested under `skills`:

```go
type SkillsConfig struct {
    SkillDirs   []string         `mapstructure:"skill_dirs"`
    Agent       SkillAgentConfig `mapstructure:"agent"`
}

type SkillAgentConfig struct {
    Binary   string `mapstructure:"binary"`   // default: same as pi.binary
    Provider string `mapstructure:"provider"` // default: same as pi.provider
    Model    string `mapstructure:"model"`    // default: same as pi.model
}
```

Defaults in `Defaults()`: inherit from `PiConfig` (same binary, provider, model). Operators can point the skill agent at a cheaper/faster model (e.g. Haiku) since it only does single-turn catalog reasoning — same pattern as `memory.agent` config.

`configs/agentloop.yaml` addition:

```yaml
skills:
    skill_dirs:
        - ~/.local/share/agentloop/vault/skills
    agent:
        binary: pi           # inherits pi.binary if blank
        provider: anthropic  # use a cheaper model for skill selection
        model: claude-haiku-4-5-20251001
```

---

### 2. `Skill` Struct & SKILL.md Format (`internal/skills/registry.go`)

**Struct changes:**

```go
type SkillFile struct {
    Name        string `yaml:"name"                  json:"name"`
    Path        string `yaml:"-"                     json:"path"`
    Description string `yaml:"description,omitempty" json:"description,omitempty"`
    Type        string `yaml:"-"                     json:"type"` // extension without dot; "" for files with no extension
}

type Skill struct {
    Name         string      `yaml:"name"`
    Description  string      `yaml:"description"`
    Tags         []string    `yaml:"tags"`
    Files        []SkillFile `yaml:"files,omitempty"`
    Instructions string      `yaml:"-" json:"instructions"`
    Dir          string      `yaml:"-" json:"dir"`
}
```

**Removed:** `Triggers []string` — replaced by `Tags []string`.

**SKILL.md format:**

```yaml
---
name: docker-deploy
description: Build and deploy Docker containers, manage images and registries
tags: [docker, deployment, container, infrastructure, build]
files:
  - name: deploy.sh
    description: Runs the full deployment pipeline
  - name: setup.ts
    description: Initialises environment variables and registry credentials
---
# Instructions body
```

The `files` frontmatter block is optional. Skills without it still work — files are auto-scanned from the directory.

**Migration:** Existing SKILL.md files with `triggers` parse without error (YAML ignores unknown fields). `triggers` becomes inert. No migration required.

**Load-time behaviour (`LoadAll`):** After parsing SKILL.md, scan the skill directory for all non-SKILL.md files. For each file:
- Set `Path` to the absolute file path
- Set `Type` to the file extension without the leading dot (e.g. `"sh"`, `"ts"`). Files with no extension (e.g. `Makefile`) get `Type: ""` and are still included
- Check if the frontmatter `files` manifest has a matching entry by `Name` — if so, inherit its `Description`

Set `Dir` to the absolute skill directory path.

**Existing `Get(name string) (*Skill, error)` is unchanged** — returns `os.ErrNotExist` when not found.

**`Registry` constructor is unchanged:** `NewRegistry(skillDirs []string) *Registry`. No `defaultPageSize` needed.

---

### 3. `SkillAgent` (`internal/skills/agent.go`)

A short-lived pi subprocess that reasons over the skill catalog and returns the best matching skill name. Modelled directly on `MetaAgent.runPiSession()`.

```go
type SkillAgent struct {
    piCfg  config.PiConfig
    secCfg config.SecurityConfig
}

func NewSkillAgent(piCfg config.PiConfig, secCfg config.SecurityConfig) *SkillAgent

// Find selects the best skill for the given query from the catalog.
// Returns the matching skill name, or "" if no skill is relevant.
func (a *SkillAgent) Find(ctx context.Context, query string, catalog []SkillCatalogEntry) (string, error)
```

**`Find()` implementation:**

1. Build a prompt from `query` + `catalog` (see prompt format below)
2. Create `bridge.New(a.piCfg, a.secCfg, config.HITLConfig{})` — no HITL for this subprocess
3. Set event handler to collect `text_delta` into a `strings.Builder`
4. `b.Start(ctx, os.TempDir())` — no real workdir needed
5. `b.Prompt(ctx, "skill-find", prompt)`
6. `<-b.Done()`
7. `b.Stop()`
8. Parse the response: extract the first skill name found in the response that exists in the catalog; if the response is "none" or no valid name is found, return `""`

**Prompt format:**

```
You are a skill selector. Given a task description and a catalog of available skills,
return the name of the single most relevant skill, or "none" if no skill applies.

Respond with ONLY the skill name or "none". No explanation.

Task: <query>

Available skills:
- name: docker-deploy
  description: Build and deploy Docker containers
  tags: [docker, deployment, container]

- name: go-testing
  description: Go test patterns and table-driven test strategies
  tags: [go, testing, tdd]
...
```

**`SkillCatalogEntry` type** (defined alongside `SkillAgent` in `agent.go`):

```go
type SkillCatalogEntry struct {
    Name        string   `json:"name"        yaml:"name"`
    Description string   `json:"description" yaml:"description"`
    Tags        []string `json:"tags"        yaml:"tags"`
}
```

**`Registry.Catalog()` method** (new, on `registry.go`): returns `[]SkillCatalogEntry` for all loaded skills. Used by the handler to build the catalog snapshot passed to `SkillAgent.Find()`.

---

### 4. Bridge IPC (`internal/bridge/`)

Follows the established `Retrieve_memory` pattern: one per-session temp file with UUID-based name, env var set before `b.Start()`, cleaned up in `Stop()`.

**New field on `PiBridge` (`rpc.go`):**

```go
skillLoadPath string // per-session temp file for Find_skill result
```

**Temp file creation (in `Start()`):**

```go
b.skillLoadPath = filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-skill-load-%s.json", uuid.New().String()[:8]))
```

**Env var set in `buildSafeEnv()`:**

| Env Var | Purpose |
|---|---|
| `AGENTLOOP_SKILL_LOAD_PATH` | Path where `Find_skill` result JSON is written |

**Cleanup in `Stop()`:**

```go
if b.skillLoadPath != "" { _ = os.Remove(b.skillLoadPath) }
```

**Handler type (`events.go`):**

```go
type SkillToolEvent struct {
    Tool         string         // "Find_skill"
    Params       map[string]any
    SkillLoadPath string
}
type SkillToolHandler func(event SkillToolEvent)
```

**New bridge method:**

```go
func (b *PiBridge) SetSkillToolHandler(h SkillToolHandler) { b.onSkillTool = h }
```

**Interception in `readEvents()`:** After the `b.onEvent` dispatch (same placement as memory tool interception), check `tool_execution_start` for `"Find_skill"` and route to `b.onSkillTool`. `Find_skill` events are **not** forwarded to `OnToolUse` — silently consumed.

---

### 5. Wiring: `Callbacks` → `core.go` → `session/manager.go`

Follows the exact same three-step mechanism as `OnMemoryTool`:

**Step 1 — Add to `agent.Callbacks` (`core.go`):**

```go
type Callbacks struct {
    // ... existing fields ...
    OnSkillTool  bridge.SkillToolHandler
}
```

**Step 2 — Wire in `core.go` `Run()` (alongside `OnMemoryTool`):**

```go
if c.cb.OnSkillTool != nil {
    b.SetSkillToolHandler(c.cb.OnSkillTool)
}
```

**Step 3 — Populate in `session/manager.go` `StartSession()` goroutine (alongside `OnMemoryTool`):**

```go
OnSkillTool: func(ev bridge.SkillToolEvent) {
    catalog := m.skills.Catalog()
    name, err := m.skillAgent.Find(ctx, ev.Params["query"].(string), catalog)
    if err != nil || name == "" {
        writeJSON(ev.SkillLoadPath, map[string]any{"error": "no matching skill found"})
        return
    }
    skill, err := m.skills.Get(name)
    if err != nil {
        writeJSON(ev.SkillLoadPath, map[string]any{"error": "skill not found: " + name})
        return
    }
    writeJSON(ev.SkillLoadPath, skill)
},
```

`m.skills *skills.Registry` already exists on the manager. `m.skillAgent *skills.SkillAgent` is a new field, constructed in `NewManager()` using the `SkillAgentConfig`.

---

### 6. TypeScript Extension (`extensions/skill-tools.ts`)

One tool registered via `pi.addTool()`:

**`Find_skill`:**
- Params: `query` (string, required) — natural language description of what the agent needs
- Description: *"Find and load the most relevant skill for your current task. Describe what you need in natural language — a side agent will search the skill catalog and return the best match with full instructions and available files. Call this when you need specialized workflow instructions for a domain like deployment, testing, migrations, etc."*
- `execute()`: reads `AGENTLOOP_SKILL_LOAD_PATH`, returns parsed JSON

**Success response shape:**

```json
{
  "name": "docker-deploy",
  "description": "Build and deploy Docker containers",
  "instructions": "...",
  "dir": "/home/user/.local/share/agentloop/vault/skills/docker-deploy",
  "files": [
    { "name": "deploy.sh", "path": "/.../.../deploy.sh", "type": "sh", "description": "Runs full deployment pipeline" },
    { "name": "Makefile",  "path": "/.../.../Makefile",  "type": "" }
  ]
}
```

**Error response shape:**

```json
{ "error": "no matching skill found" }
```

The agent reads the error and decides whether to proceed without a skill.

No unit test file for the TS extension — same integration approach as `memory-tools.ts`.

---

## Data Flow

```
Main pi agent decides it needs specialized guidance
    │
    ▼
Find_skill(query: "deploy docker container to production")
    │
    ▼
bridge readEvents() intercepts tool_execution_start (post-dispatch, like memory tools)
    │
    ▼
OnSkillTool handler (manager.go):
    ├─ registry.Catalog() → []SkillCatalogEntry
    ├─ skillAgent.Find(ctx, query, catalog)
    │      └─ side pi subprocess (own context window):
    │           prompt: catalog + query → responds with skill name or "none"
    │           <-b.Done()
    ├─ registry.Get(selectedName)
    └─ writeJSON(skillLoadPath, fullSkill)
    │
    ▼
TS execute() reads AGENTLOOP_SKILL_LOAD_PATH → returns full skill to main agent
    │
    ▼
Main agent reads instructions + file listing, invokes files via bash/read tools
```

---

## What Is Removed

| Item | Location | Reason |
|---|---|---|
| `Triggers []string` | `Skill` struct | Replaced by `Tags` |
| `DetectSkills()` | `prompt_builder.go` | No longer needed |
| `skills *skills.Registry` field + param | `PromptBuilder` / `NewPromptBuilder` | No longer needed |
| `skillNames []string` param | `PromptBuilder.Build()` | No longer needed |
| Skill injection loop | `prompt_builder.go` | Skills loaded on demand via tool |
| `skillNames` call | `core.go` | No longer needed |
| `o.skillsReg` arg in `NewPromptBuilder` calls | `orchestrator.go` (×2) | No longer needed |

---

## Additional Updates

- **`cmd/agentloop-server/main.go`:** Construct `skills.NewSkillAgent(piCfg, secCfg)` and pass to `session.NewManager()`
- **`internal/session/manager.go`:** Add `skillAgent *skills.SkillAgent` field; construct in `NewManager()`
- **`CLAUDE.md`:** Update Skills Registry section — describe tool-based discovery via `Find_skill` and `SkillAgent`; remove references to `DetectSkills()` and trigger-based matching

---

## Config Reference

| Key | Type | Default | Description |
|---|---|---|---|
| `skills.skill_dirs` | `[]string` | `[~/.local/share/agentloop/vault/skills]` | Directories scanned for SKILL.md files |
| `skills.agent.binary` | `string` | inherits `pi.binary` | Pi binary for the skill side agent |
| `skills.agent.provider` | `string` | inherits `pi.provider` | LLM provider for skill selection |
| `skills.agent.model` | `string` | inherits `pi.model` | Model for skill selection — use a cheap/fast model |

---

## Testing

- `TestSkillAgentFindMatch` — catalog with matching skill, agent returns correct name
- `TestSkillAgentFindNoMatch` — catalog with no relevant skills, agent returns `""`
- `TestSkillAgentFindNone` — agent responds "none", returns `""`
- `TestRegistryCatalog` — `Catalog()` returns all skills as `SkillCatalogEntry` (no instructions)
- `TestSkillLoadWithFiles` — auto-scan files, frontmatter description inheritance, absolute paths set
- `TestSkillFileNoExtension` — files with no extension get `Type: ""` and are included
- `TestSkillLoadMissingName` — falls back to directory name
- Existing `registry.Get()` and `registry.List()` tests remain valid

---

## Non-Goals

- No pagination in the main agent — the side agent handles catalog reasoning in its own context
- No skill ranking exposed to the main agent — side agent returns a single selection
- No skill auto-execution — main agent invokes files explicitly via pi's existing tools
- No changes to HITL, memory, or session systems
