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

Let the pi agent discover and load skills autonomously at runtime via tool calls, based on semantic understanding rather than keyword matching. Keep the context window compact — no skill catalog in the system prompt.

---

## Architecture

### Overview

Two new pi tools exposed via a TypeScript extension (`skill-tools.ts`), following the established `memory-tools.ts` IPC pattern:

1. **`Search_skills`** — paginated browse/filter of available skills (metadata only)
2. **`Use_skill`** — load full skill details for a selected skill by name

The agent uses them like an e-commerce category page: filter by query, paginate to find the right skill, then load the full details.

### Agent Interaction Pattern

```
Agent: Search_skills(query: "docker deployment", page: 1)
→ { skills: [{name, description, tags}, ...], current_page: 1, total_pages: 2, total_results: 14 }

Agent: Search_skills(query: "docker deployment", page: 2)
→ { skills: [...], current_page: 2, total_pages: 2, total_results: 14 }

Agent: Use_skill(name: "docker-deploy")
→ { name, description, instructions, dir, files: [{name, path, type, description?}, ...] }
```

---

## Component Changes

### 1. Config — `skills` Section (`internal/config/config.go` + `configs/agentloop.yaml`)

Add `search_page_size` to the existing `SkillsConfig` struct:

```go
type SkillsConfig struct {
    SkillDirs      []string `mapstructure:"skill_dirs"`
    SearchPageSize int      `mapstructure:"search_page_size"` // default: 10
}
```

Default in `Defaults()`: `SearchPageSize: 10`

`configs/agentloop.yaml` addition:

```yaml
skills:
    skill_dirs:
        - ~/.local/share/agentloop/vault/skills
    search_page_size: 10  # number of skills returned per Search_skills page
```

The registry receives `SearchPageSize` at construction and uses it as the default `pageSize` when the agent does not supply one. The agent can override `page_size` per call (capped at 50).

---

### 2. `Skill` Struct & SKILL.md Format (`internal/skills/registry.go`)

**Struct changes:**

```go
type SkillFile struct {
    Name        string `yaml:"name"                  json:"name"`
    Path        string `yaml:"-"                     json:"path"`        // absolute path, set at load time
    Description string `yaml:"description,omitempty" json:"description,omitempty"`
    Type        string `yaml:"-"                     json:"type"`        // file extension without dot; empty string for files with no extension
}

type Skill struct {
    Name         string      `yaml:"name"`
    Description  string      `yaml:"description"`
    Tags         []string    `yaml:"tags"`            // replaces Triggers
    Files        []SkillFile `yaml:"files,omitempty"` // optional manifest from frontmatter
    Instructions string      `yaml:"-"                json:"instructions"`
    Dir          string      `yaml:"-"                json:"dir"`         // absolute path to skill directory
}
```

**Removed:** `Triggers []string`

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

**Load-time behaviour (`LoadAll`):** After parsing SKILL.md, scan the skill directory for all non-SKILL.md files. For each file found:
- Set `Path` to the absolute file path
- Set `Type` to the file extension without the leading dot (e.g. `"sh"`, `"ts"`). Files with no extension (e.g. `Makefile`) get `Type: ""` and are still included in the listing
- Check if the frontmatter `files` manifest has a matching entry by `Name` — if so, inherit its `Description`

Set `Dir` to the absolute skill directory path.

---

### 3. Registry Search (`internal/skills/registry.go`)

**New types:**

```go
type SkillCatalogEntry struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Tags        []string `json:"tags"`
}

type SearchResult struct {
    Skills       []SkillCatalogEntry `json:"skills"`
    CurrentPage  int                 `json:"current_page"`
    TotalPages   int                 `json:"total_pages"`
    TotalResults int                 `json:"total_results"`
}
```

**New method `Search`:**

```go
func (r *Registry) Search(query string, page int, pageSize int) SearchResult
```

- Tokenizes `query` into lowercase words
- A skill matches if any query word is a substring of the skill's lowercased name, description, or any tag
- Empty query matches all skills (full browse)
- Paginates the filtered results by `page` (1-based) and `pageSize`
- `pageSize` is capped at 50; if agent passes 0 or negative, falls back to the configured default stored on `Registry`
- Returns `SearchResult` with metadata-only entries (no instructions, no file contents)

**Filter intent:** Basic word-inclusion narrowing only — not ranking. The LLM makes the semantic selection from the returned page.

**Existing `Get(name string) (*Skill, error)` is unchanged.** It already exists and returns `os.ErrNotExist` when the skill is not found. After the struct refactor, it returns the enriched `Skill` (with `Dir`, `Files`) automatically.

**`Registry` constructor change:**

```go
func NewRegistry(skillDirs []string, defaultPageSize int) *Registry
```

Stores `defaultPageSize` on the struct for use when the agent omits `page_size`.

**Callers to update:**
- `cmd/agentloop-server/main.go` line 87: `skills.NewRegistry(cfg.Skills.SkillDirs)` → `skills.NewRegistry(cfg.Skills.SkillDirs, cfg.Skills.SearchPageSize)`

---

### 4. Bridge IPC (`internal/bridge/`)

Follows the established `Retrieve_memory` pattern exactly: two per-session temp files with UUID-based names, env vars set before `b.Start()`, cleaned up in `Stop()`.

**New fields on `PiBridge` (`rpc.go`):**

```go
skillSearchPath string // per-session temp file for Search_skills results
skillLoadPath   string // per-session temp file for Use_skill results
onSkillTool     SkillToolHandler
```

**Temp file creation (in `Start()`, alongside `retrievePath`):**

```go
b.skillSearchPath = filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-skill-search-%s.json", uuid.New().String()[:8]))
b.skillLoadPath   = filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-skill-load-%s.json",   uuid.New().String()[:8]))
```

**Env vars set in `buildSafeEnv()` (alongside `AGENTLOOP_RETRIEVE_PATH`):**

| Env Var | Purpose |
|---|---|
| `AGENTLOOP_SKILL_SEARCH_PATH` | Path where `Search_skills` results JSON is written |
| `AGENTLOOP_SKILL_LOAD_PATH` | Path where `Use_skill` result JSON is written |

**Cleanup in `Stop()` (alongside `retrievePath`):**

```go
if b.skillSearchPath != "" { _ = os.Remove(b.skillSearchPath) }
if b.skillLoadPath   != "" { _ = os.Remove(b.skillLoadPath) }
```

**Handler type (`events.go`):**

```go
type SkillToolEvent struct {
    Tool           string         // "Search_skills" or "Use_skill"
    Params         map[string]any // raw params from pi
    SkillSearchPath string
    SkillLoadPath   string
}
type SkillToolHandler func(event SkillToolEvent)
```

**New bridge method (`rpc.go`):**

```go
func (b *PiBridge) SetSkillToolHandler(h SkillToolHandler) { b.onSkillTool = h }
```

**Interception in `readEvents()`:** After the `b.onEvent` dispatch (same placement as memory tool interception), check if `event.Type == "tool_execution_start"` and `event.ToolName` is `"Search_skills"` or `"Use_skill"`. If so, call `b.onSkillTool(SkillToolEvent{...})`. `Search_skills` and `Use_skill` tool events are **not** forwarded to `OnToolUse` — they are silently consumed by the bridge interception, keeping them invisible to clients.

**Handler wiring:** Follows the exact same mechanism as `OnMemoryTool`:

1. Add `OnSkillTool bridge.SkillToolHandler` field to `agent.Callbacks` struct in `core.go`
2. Wire it in `core.go`'s `Run()` (alongside the existing `OnMemoryTool` wire-up):
   ```go
   if c.cb.OnSkillTool != nil {
       b.SetSkillToolHandler(c.cb.OnSkillTool)
   }
   ```
3. Populate `OnSkillTool` in `session/manager.go`'s `StartSession()` goroutine within the `Callbacks` literal (alongside `OnMemoryTool`):

```go
OnSkillTool: func(ev bridge.SkillToolEvent) {
    switch ev.Tool {
    case "Search_skills":
        query, _ := ev.Params["query"].(string)
        page := intParam(ev.Params, "page", 1)
        pageSize := intParam(ev.Params, "page_size", 0) // 0 = use registry default
        result := m.skills.Search(query, page, pageSize)
        writeJSON(ev.SkillSearchPath, result)
    case "Use_skill":
        name, _ := ev.Params["name"].(string)
        skill, err := m.skills.Get(name)
        if err != nil {
            writeJSON(ev.SkillLoadPath, map[string]any{"error": "skill not found: " + name})
            return
        }
        writeJSON(ev.SkillLoadPath, skill)
    }
},
```

`m.skills *skills.Registry` already exists on the session manager — no new field needed.

**Error response shape** (when `Use_skill` name is not found):
```json
{ "error": "skill not found: <name>" }
```

The TS `execute()` returns this object to pi; the agent reads the error message and can refine its search.

---

### 5. `PromptBuilder` & `Orchestrator` Cleanup

**`PromptBuilder` no longer needs the registry.** After removing `DetectSkills()` and the `skillNames` injection loop, the `skills *skills.Registry` field and parameter are dropped entirely.

**Changes to `prompt_builder.go`:**
- Remove `skills *skills.Registry` field from `PromptBuilder` struct
- Remove `sk *skills.Registry` parameter from `NewPromptBuilder`
- Remove `DetectSkills()` method
- Remove `skillNames []string` parameter from `Build()`
- Remove `<skill>` injection loop from `Build()`

**`Build()` signature after:**

```go
func (pb *PromptBuilder) Build(userId string, task string, conversationContextID string) (string, error)
func NewPromptBuilder(mem *memory.Engine) *PromptBuilder
```

**Changes to `core.go`:**
- Remove `skillNames := c.pb.DetectSkills(task)` call
- Remove `skillNames` argument from `pb.Build()`

**Changes to `orchestrator.go`:** Two callsites of `NewPromptBuilder` must drop the `o.skillsReg` argument:
- Line 182: `pb := NewPromptBuilder(o.memoryEngine, o.skillsReg)` → `NewPromptBuilder(o.memoryEngine)`
- Line 392: `pb := NewPromptBuilder(o.memoryEngine, o.skillsReg)` → `NewPromptBuilder(o.memoryEngine)`

The `skillsReg *skills.Registry` field on `Orchestrator` is not removed — it is still used by the session manager for the skill tool handler. Only the `PromptBuilder` dependency is dropped.

---

### 6. TypeScript Extension (`extensions/skill-tools.ts`)

Two tools registered via `pi.addTool()`, following the same style as `memory-tools.ts`:

**`Search_skills`:**
- Params: `query` (string, optional), `page` (number, default 1), `page_size` (number, optional)
- Description: *"Browse available skills by category or keyword. Use when you need specialized instructions for a task domain — deployment, testing, migration, etc. Returns paginated results; refine your query or increase the page to find the right skill."*
- `execute()`: reads `AGENTLOOP_SKILL_SEARCH_PATH`, returns parsed JSON

**`Use_skill`:**
- Params: `name` (string, required)
- Description: *"Load full instructions and file listing for a skill by name. Call after identifying a relevant skill via Search_skills. The returned instructions and files guide you through the specialized workflow."*
- `execute()`: reads `AGENTLOOP_SKILL_LOAD_PATH`, returns parsed JSON

**`Use_skill` success response shape:**

```json
{
  "name": "docker-deploy",
  "description": "Build and deploy Docker containers",
  "instructions": "...",
  "dir": "/home/user/.local/share/agentloop/vault/skills/docker-deploy",
  "files": [
    { "name": "deploy.sh", "path": "/.../.../deploy.sh", "type": "sh", "description": "Runs full deployment pipeline" },
    { "name": "setup.ts", "path": "/.../.../setup.ts", "type": "ts" },
    { "name": "Makefile",  "path": "/.../.../Makefile",  "type": "" }
  ]
}
```

The agent receives `dir` (skill root), full file listing with absolute paths, optional per-file descriptions, and full instructions. It then uses pi's existing `Read`/`Bash` tools to inspect or execute any file.

No unit test file for the TS extension — integration testing follows the same approach as `memory-tools.ts`.

---

## Data Flow

```
pi agent decides it needs skill guidance
    │
    ▼
Search_skills(query="...", page=1)
    │
    ▼
bridge readEvents() intercepts tool_execution_start (post-dispatch, like memory tools)
    │
    ├─ calls onSkillTool handler (set by session/manager.go)
    ├─ handler: registry.Search(query, page, pageSize)
    ├─ marshal SearchResult → write to skillSearchPath
    │
    ▼
TS execute() reads AGENTLOOP_SKILL_SEARCH_PATH → returns SearchResult to pi
    │
    ▼
Agent reads page, may paginate, selects skill name
    │
    ▼
Use_skill(name="docker-deploy")
    │
    ▼
bridge readEvents() intercepts tool_execution_start
    │
    ├─ handler: registry.Get(name)
    ├─ on error: write {"error": "skill not found: ..."} to skillLoadPath
    ├─ on success: marshal full Skill → write to skillLoadPath
    │
    ▼
TS execute() reads AGENTLOOP_SKILL_LOAD_PATH → returns result to pi
    │
    ▼
Agent reads instructions + file listing, invokes files via pi's bash/read tools
```

---

## What Is Removed

| Item | Location | Reason |
|---|---|---|
| `Triggers []string` | `Skill` struct | Replaced by `Tags` |
| `DetectSkills()` | `prompt_builder.go` | No longer needed |
| `skills *skills.Registry` field + param | `PromptBuilder` / `NewPromptBuilder` | No longer needed |
| `skillNames` param | `PromptBuilder.Build()` | No longer needed |
| Skill injection loop | `prompt_builder.go` | Skills loaded on demand via tool |
| `skillNames` call | `core.go` | No longer needed |
| `o.skillsReg` arg in `NewPromptBuilder` calls | `orchestrator.go` (×2) | No longer needed |

---

## Config Reference

| Key | Type | Default | Description |
|---|---|---|---|
| `skills.skill_dirs` | `[]string` | `[~/.local/share/agentloop/vault/skills]` | Directories scanned for SKILL.md files |
| `skills.search_page_size` | `int` | `10` | Default number of skills per `Search_skills` page (agent-overridable up to 50) |

---

## Additional Updates

- **`cmd/agentloop-server/main.go`:** Update `skills.NewRegistry(cfg.Skills.SkillDirs)` → `skills.NewRegistry(cfg.Skills.SkillDirs, cfg.Skills.SearchPageSize)`
- **`CLAUDE.md`:** Update the Skills Registry section to describe the new tool-based discovery mechanism; remove references to `DetectSkills()` and trigger-based matching

---

## Testing

- `TestRegistrySearch` — query filtering, pagination, empty query returns all
- `TestRegistrySearchEmptyRegistry` — graceful empty result
- `TestRegistrySearchPageSize` — default page size from config, agent override, cap at 50
- `TestSkillLoadWithFiles` — auto-scan files, frontmatter description inheritance, absolute paths set, `Type` correctly derived
- `TestSkillFileNoExtension` — files with no extension get `Type: ""` and are included
- `TestSkillLoadMissingName` — falls back to directory name
- `TestSearchResultPagination` — page/pageSize boundaries, `total_pages` calculation
- `TestRegistryNewConstructor` — `defaultPageSize` stored and used correctly
- Existing `registry.Get()` and `registry.List()` tests remain valid

---

## Non-Goals

- No semantic/vector embedding search — the LLM handles semantic matching from the filtered page
- No skill ranking — filter narrows, LLM selects
- No skill auto-execution — agent invokes files explicitly via pi's existing tools
- No changes to HITL, memory, or session systems
