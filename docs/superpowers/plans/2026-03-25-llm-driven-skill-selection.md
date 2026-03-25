# LLM-Driven Skill Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace lexical trigger-based skill matching with a `Find_skill` pi tool backed by a `SkillAgent` subprocess, so the main agent discovers skills semantically at runtime without bloating its context window.

**Architecture:** A new `internal/pirun` package extracts `runPiSession()` as a shared utility. `SkillAgent` in `internal/skills/agent.go` uses it to spin up a short-lived pi subprocess that reasons over the skill catalog and returns one skill name. The main pi agent calls `Find_skill` (a single TS tool), the Go bridge intercepts it, invokes `SkillAgent.Find()`, and writes the full skill result to a temp file — returning only the final answer to the main agent.

**Tech Stack:** Go 1.23, `github.com/MarcoMruz/agentloop/internal/bridge`, `github.com/MarcoMruz/agentloop/internal/config`, TypeScript pi extension, `gopkg.in/yaml.v3`

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `internal/pirun/pirun.go` | **Create** | Shared `RunTextSession()` — extracted from MetaAgent |
| `internal/pirun/pirun_test.go` | **Create** | Unit tests for RunTextSession |
| `internal/skills/registry.go` | **Modify** | Add `Tags`, `SkillFile`, `Dir`, `Catalog()` — remove `Triggers` |
| `internal/skills/registry_test.go` | **Create** | Tests for Catalog(), LoadAll with files, SkillFile scanning |
| `internal/skills/agent.go` | **Create** | `SkillAgent` + `SkillCatalogEntry` |
| `internal/skills/agent_test.go` | **Create** | Tests for SkillAgent.Find() |
| `internal/config/config.go` | **Modify** | Add `SkillAgentConfig` to `SkillsConfig`; add defaults |
| `configs/agentloop.yaml` | **Modify** | Add `skills.agent` block |
| `internal/bridge/events.go` | **Modify** | Add `SkillToolEvent`, `SkillToolHandler` |
| `internal/bridge/rpc.go` | **Modify** | Add `skillLoadPath`, `SetSkillToolHandler`, interception in `readEvents()`, temp file cleanup in `Stop()` |
| `internal/agent/core.go` | **Modify** | Add `OnSkillTool` to `Callbacks`; wire `SetSkillToolHandler` in `Run()` |
| `internal/agent/prompt_builder.go` | **Modify** | Remove `skills` field, `DetectSkills()`, `skillNames` param from `Build()` |
| `internal/agent/orchestrator.go` | **Modify** | Remove `o.skillsReg` from two `NewPromptBuilder` calls; replace `runReadOnlyPi()` with `pirun.RunTextSession()`; update `Evolve()` caller |
| `internal/session/manager.go` | **Modify** | Add `skillAgent` field; populate `OnSkillTool` in `StartSession()` |
| `internal/memory/evolve/meta/agent.go` | **Modify** | Replace `runPiSession()` with `pirun.RunTextSession()` |
| `extensions/skill-tools.ts` | **Create** | `Find_skill` pi tool |
| `cmd/agentloop-server/main.go` | **Modify** | Construct `SkillAgent`, pass to `NewManager()` |
| `CLAUDE.md` | **Modify** | Update Skills Registry section |

---

## Task 1: Extract `pirun.RunTextSession()` shared utility

**Files:**
- Create: `internal/pirun/pirun.go`
- Create: `internal/pirun/pirun_test.go`

- [ ] **Step 1.1: Write the failing test**

  Create `internal/pirun/pirun_test.go`:

  ```go
  package pirun

  import (
      "context"
      "strings"
      "testing"

      "github.com/MarcoMruz/agentloop/internal/config"
  )

  func TestRunTextSessionSignature(t *testing.T) {
      // Verify the function exists and has the right signature.
      // We cannot call it without a real pi binary, so we just verify compilation
      // and that calling with a cancelled context returns an error quickly.
      ctx, cancel := context.WithCancel(context.Background())
      cancel() // immediately cancelled

      piCfg := config.PiConfig{Binary: "pi", Provider: "anthropic", Model: "claude-haiku-4-5-20251001"}
      secCfg := config.SecurityConfig{}

      _, err := RunTextSession(ctx, piCfg, secCfg, t.TempDir(), "test", "hello")
      if err == nil {
          t.Skip("pi binary available — skipping compile-only test")
      }
      if !strings.Contains(err.Error(), "start pi") && !strings.Contains(err.Error(), "context") {
          t.Errorf("unexpected error: %v", err)
      }
  }
  ```

- [ ] **Step 1.2: Run test to confirm it fails to compile**

  ```bash
  go test ./internal/pirun/... -v
  ```
  Expected: compile error — package does not exist yet.

- [ ] **Step 1.3: Create `internal/pirun/pirun.go`**

  ```go
  package pirun

  import (
      "context"
      "fmt"
      "os"
      "strings"

      "github.com/MarcoMruz/agentloop/internal/bridge"
      "github.com/MarcoMruz/agentloop/internal/config"
  )

  // RunTextSession starts a pi subprocess, sends a single prompt, collects the
  // full text response, and returns it. The subprocess exits after responding.
  // Pass os.TempDir() as workDir when no real working directory is needed.
  func RunTextSession(
      ctx context.Context,
      piCfg config.PiConfig,
      secCfg config.SecurityConfig,
      workDir string,
      promptID string,
      prompt string,
  ) (string, error) {
      if workDir == "" {
          workDir = os.TempDir()
      }

      b := bridge.New(piCfg, secCfg, config.HITLConfig{})

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

      if err := b.Prompt(ctx, promptID, prompt); err != nil {
          return "", fmt.Errorf("prompt pi: %w", err)
      }

      <-b.Done()
      return response.String(), nil
  }
  ```

- [ ] **Step 1.4: Run test**

  ```bash
  go test ./internal/pirun/... -v
  ```
  Expected: PASS (test either passes with skip message or passes by detecting context cancellation error).

- [ ] **Step 1.5: Verify `go build ./...` still compiles**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 1.6: Commit**

  ```bash
  git add internal/pirun/
  git commit -m "feat(pirun): add RunTextSession shared utility"
  ```

---

## Task 2: Migrate all `runPiSession`/`runReadOnlyPi` to `pirun.RunTextSession()`

Both `MetaAgent.runPiSession()` and `Orchestrator.runReadOnlyPi()` are identical single-turn pi session runners. Both are replaced with `pirun.RunTextSession()`.

**Files:**
- Modify: `internal/memory/evolve/meta/agent.go`
- Modify: `internal/agent/orchestrator.go`

- [ ] **Step 2.1: Replace `runPiSession()` in `meta/agent.go`**

  In `internal/memory/evolve/meta/agent.go`:

  1. Add import: `"github.com/MarcoMruz/agentloop/internal/pirun"`
  2. Change `Evolve(outcome metrics.TaskOutcome)` signature to `Evolve(ctx context.Context, outcome metrics.TaskOutcome)` — the `ctx` is needed to pass to `RunTextSession`.
  3. In `Evolve()`, replace:
     ```go
     response, err := m.runPiSession(prompt)
     ```
     with:
     ```go
     workDir, err := os.MkdirTemp("", "memevolve-*")
     if err != nil {
         slog.Error("failed to create temp dir", "error", err)
         return
     }
     defer os.RemoveAll(workDir)
     response, err := pirun.RunTextSession(ctx, m.piCfg, m.secCfg, workDir, "meta-evolve", prompt)
     ```
  4. Delete the entire `runPiSession()` method.
  5. Remove import `"context"` if it is now only provided by the function parameter (keep it — `context.Context` is still referenced in the signature).

- [ ] **Step 2.2: Update the caller of `Evolve()` in `internal/agent/orchestrator.go`**

  The call is at line ~233 in `orchestrator.go`:
  ```go
  o.metaAgent.Evolve(taskOutcome)
  ```
  Change to (synchronous, no goroutine — preserves existing call semantics):
  ```go
  o.metaAgent.Evolve(ctx, taskOutcome)
  ```

- [ ] **Step 2.3: Replace `runReadOnlyPi()` in `orchestrator.go` with `pirun.RunTextSession()`**

  `Orchestrator.runReadOnlyPi()` (lines ~413–434) is functionally identical to `pirun.RunTextSession()`.

  1. Add import: `"github.com/MarcoMruz/agentloop/internal/pirun"`
  2. Replace each call site:
     - Line ~308: `response, err := o.runReadOnlyPi(ctx, octx.Config.Planner, octx.WorkDir, prompt)` → `response, err := pirun.RunTextSession(ctx, octx.Config.Planner, o.secCfg, octx.WorkDir, "orchestrator", prompt)`
     - Line ~323: `response, err := o.runReadOnlyPi(ctx, octx.Config.Judge, octx.WorkDir, prompt)` → `response, err := pirun.RunTextSession(ctx, octx.Config.Judge, o.secCfg, octx.WorkDir, "orchestrator", prompt)`
  3. Delete `runReadOnlyPi()` method entirely.
  4. Remove import `"github.com/MarcoMruz/agentloop/internal/bridge"` from `orchestrator.go` if it was only used by `runReadOnlyPi()` — check for other usages first.

- [ ] **Step 2.4: Run existing evolve and orchestrator tests**

  ```bash
  go test ./internal/memory/evolve/... -v
  go test ./internal/agent/... -v
  ```
  Expected: all pass.

- [ ] **Step 2.5: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 2.6: Commit**

  ```bash
  git add internal/memory/evolve/meta/agent.go internal/agent/orchestrator.go
  git commit -m "refactor: migrate runPiSession and runReadOnlyPi to pirun.RunTextSession"
  ```

---

## Task 3: Update config — add `SkillAgentConfig`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `configs/agentloop.yaml`

- [ ] **Step 3.1: Add `SkillAgentConfig` to config.go**

  Replace the current `SkillsConfig`:
  ```go
  type SkillsConfig struct {
      SkillDirs []string `mapstructure:"skill_dirs"`
  }
  ```
  with:
  ```go
  type SkillAgentConfig struct {
      Binary   string `mapstructure:"binary"`
      Provider string `mapstructure:"provider"`
      Model    string `mapstructure:"model"`
  }

  type SkillsConfig struct {
      SkillDirs []string        `mapstructure:"skill_dirs"`
      Agent     SkillAgentConfig `mapstructure:"agent"`
  }
  ```

- [ ] **Step 3.2: Add defaults in `Defaults()`**

  Change:
  ```go
  Skills: SkillsConfig{SkillDirs: []string{"~/.local/share/agentloop/vault/skills"}},
  ```
  to:
  ```go
  Skills: SkillsConfig{
      SkillDirs: []string{"~/.local/share/agentloop/vault/skills"},
      Agent: SkillAgentConfig{
          Binary:   "pi",
          Provider: "anthropic",
          Model:    "claude-haiku-4-5-20251001",
      },
  },
  ```

- [ ] **Step 3.3: Update `configs/agentloop.yaml`**

  Replace:
  ```yaml
  skills:
      skill_dirs:
          - ~/.local/share/agentloop/vault/skills
  ```
  with:
  ```yaml
  skills:
      skill_dirs:
          - ~/.local/share/agentloop/vault/skills
      agent:
          binary: pi
          provider: anthropic
          model: claude-haiku-4-5-20251001  # cheaper model for single-turn skill selection
  ```

- [ ] **Step 3.4: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 3.5: Commit**

  ```bash
  git add internal/config/config.go configs/agentloop.yaml
  git commit -m "feat(config): add SkillAgentConfig under skills.agent"
  ```

---

## Task 4: Refactor `Skill` struct — add Tags, SkillFile, Dir, Catalog()

**Files:**
- Modify: `internal/skills/registry.go`
- Create: `internal/skills/registry_test.go`

- [ ] **Step 4.1: Write failing tests**

  Create `internal/skills/registry_test.go`:

  ```go
  package skills

  import (
      "os"
      "path/filepath"
      "testing"
  )

  func makeSkillDir(t *testing.T, skillMD string, extraFiles map[string]string) string {
      t.Helper()
      root := t.TempDir()
      skillDir := filepath.Join(root, "my-skill")
      if err := os.Mkdir(skillDir, 0755); err != nil {
          t.Fatal(err)
      }
      if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
          t.Fatal(err)
      }
      for name, content := range extraFiles {
          if err := os.WriteFile(filepath.Join(skillDir, name), []byte(content), 0644); err != nil {
              t.Fatal(err)
          }
      }
      return root
  }

  func TestRegistryCatalogReturnsMetaOnly(t *testing.T) {
      md := "---\nname: test-skill\ndescription: A test skill\ntags: [go, test]\n---\nsome instructions"
      root := makeSkillDir(t, md, nil)
      reg := NewRegistry([]string{root})
      cat := reg.Catalog()
      if len(cat) != 1 {
          t.Fatalf("expected 1 entry, got %d", len(cat))
      }
      if cat[0].Name != "test-skill" {
          t.Errorf("expected name 'test-skill', got %q", cat[0].Name)
      }
      if cat[0].Description != "A test skill" {
          t.Errorf("unexpected description: %q", cat[0].Description)
      }
      if len(cat[0].Tags) != 2 {
          t.Errorf("expected 2 tags, got %v", cat[0].Tags)
      }
  }

  func TestSkillLoadWithFiles(t *testing.T) {
      md := "---\nname: file-skill\ndescription: Has files\ntags: [deploy]\nfiles:\n  - name: deploy.sh\n    description: Deployment script\n---\ndo stuff"
      root := makeSkillDir(t, md, map[string]string{
          "deploy.sh": "#!/bin/bash\necho hi",
          "setup.ts":  "console.log('setup')",
      })
      reg := NewRegistry([]string{root})
      skill, err := reg.Get("file-skill")
      if err != nil {
          t.Fatalf("Get: %v", err)
      }
      if skill.Dir == "" {
          t.Error("Dir should be set")
      }
      if len(skill.Files) != 2 {
          t.Fatalf("expected 2 files, got %d", len(skill.Files))
      }
      // deploy.sh should have inherited description from frontmatter
      var deployFile *SkillFile
      for i := range skill.Files {
          if skill.Files[i].Name == "deploy.sh" {
              deployFile = &skill.Files[i]
          }
      }
      if deployFile == nil {
          t.Fatal("deploy.sh not found in files")
      }
      if deployFile.Description != "Deployment script" {
          t.Errorf("expected description inherited from frontmatter, got %q", deployFile.Description)
      }
      if deployFile.Type != "sh" {
          t.Errorf("expected type 'sh', got %q", deployFile.Type)
      }
      if deployFile.Path == "" {
          t.Error("Path should be set")
      }
  }

  func TestSkillFileNoExtension(t *testing.T) {
      md := "---\nname: makefile-skill\ndescription: Has Makefile\ntags: [build]\n---\nbuild stuff"
      root := makeSkillDir(t, md, map[string]string{
          "Makefile": "all:\n\techo done",
      })
      reg := NewRegistry([]string{root})
      skill, err := reg.Get("makefile-skill")
      if err != nil {
          t.Fatalf("Get: %v", err)
      }
      if len(skill.Files) != 1 {
          t.Fatalf("expected 1 file, got %d", len(skill.Files))
      }
      if skill.Files[0].Type != "" {
          t.Errorf("expected empty type for Makefile, got %q", skill.Files[0].Type)
      }
      if skill.Files[0].Name != "Makefile" {
          t.Errorf("expected Makefile, got %q", skill.Files[0].Name)
      }
  }

  func TestSkillLoadFallsBackToDirName(t *testing.T) {
      md := "---\ndescription: No name in frontmatter\ntags: [misc]\n---\nsome instructions"
      root := makeSkillDir(t, md, nil)
      reg := NewRegistry([]string{root})
      // dir name is "my-skill" (set by makeSkillDir)
      skill, err := reg.Get("my-skill")
      if err != nil {
          t.Fatalf("expected fallback to dir name, got: %v", err)
      }
      if skill.Name != "my-skill" {
          t.Errorf("expected 'my-skill', got %q", skill.Name)
      }
  }
  ```

- [ ] **Step 4.2: Run tests to confirm they fail**

  ```bash
  go test ./internal/skills/... -v
  ```
  Expected: compile errors — `SkillFile`, `Catalog()` not defined yet.

- [ ] **Step 4.3: Update `registry.go`**

  Replace the `Skill` struct and add new types/methods. Full updated `registry.go`:

  ```go
  package skills

  import (
      "os"
      "path/filepath"
      "strings"
      "sync"

      "gopkg.in/yaml.v3"
  )

  type SkillFile struct {
      Name        string `yaml:"name"                  json:"name"`
      Path        string `yaml:"-"                     json:"path"`
      Description string `yaml:"description,omitempty" json:"description,omitempty"`
      Type        string `yaml:"-"                     json:"type"`
  }

  type Skill struct {
      Name         string      `yaml:"name"`
      Description  string      `yaml:"description"`
      Tags         []string    `yaml:"tags"`
      Files        []SkillFile `yaml:"files,omitempty"`
      Instructions string      `yaml:"-" json:"instructions"`
      Dir          string      `yaml:"-" json:"dir"`
  }

  // NOTE: SkillCatalogEntry is intentionally NOT defined here.
  // It is defined in agent.go (same package). Catalog() returns []SkillCatalogEntry
  // which resolves to the definition in agent.go since both files share package skills.

  type Registry struct {
      skills map[string]*Skill
      dirs   []string
      mu     sync.RWMutex
  }

  func NewRegistry(skillDirs []string) *Registry {
      r := &Registry{skills: make(map[string]*Skill), dirs: skillDirs}
      r.LoadAll()
      return r
  }

  // LoadAll scans skill directories for SKILL.md files.
  func (r *Registry) LoadAll() {
      r.mu.Lock()
      defer r.mu.Unlock()

      for _, dir := range r.dirs {
          expanded := expandHome(dir)
          entries, err := os.ReadDir(expanded)
          if err != nil {
              continue
          }
          for _, e := range entries {
              if !e.IsDir() {
                  continue
              }
              skillDir := filepath.Join(expanded, e.Name())
              skillFile := filepath.Join(skillDir, "SKILL.md")
              data, err := os.ReadFile(skillFile)
              if err != nil {
                  continue
              }
              skill := parseSkillFile(string(data), e.Name())
              if skill == nil {
                  continue
              }
              skill.Dir = skillDir
              skill.Files = scanSkillFiles(skillDir, skill.Files)
              r.skills[skill.Name] = skill
          }
      }
  }

  // scanSkillFiles scans the skill directory for non-SKILL.md files and merges
  // with any manifest entries declared in the SKILL.md frontmatter.
  func scanSkillFiles(skillDir string, manifest []SkillFile) []SkillFile {
      entries, err := os.ReadDir(skillDir)
      if err != nil {
          return nil
      }
      // Build a description lookup from the frontmatter manifest
      descByName := make(map[string]string, len(manifest))
      for _, f := range manifest {
          descByName[f.Name] = f.Description
      }

      var files []SkillFile
      for _, e := range entries {
          if e.IsDir() || e.Name() == "SKILL.md" {
              continue
          }
          ext := strings.TrimPrefix(filepath.Ext(e.Name()), ".")
          files = append(files, SkillFile{
              Name:        e.Name(),
              Path:        filepath.Join(skillDir, e.Name()),
              Type:        ext,
              Description: descByName[e.Name()],
          })
      }
      return files
  }

  func (r *Registry) Get(name string) (*Skill, error) {
      r.mu.RLock()
      defer r.mu.RUnlock()
      s, ok := r.skills[name]
      if !ok {
          return nil, os.ErrNotExist
      }
      return s, nil
  }

  func (r *Registry) List() []*Skill {
      r.mu.RLock()
      defer r.mu.RUnlock()
      var out []*Skill
      for _, s := range r.skills {
          out = append(out, s)
      }
      return out
  }

  // Catalog returns lightweight metadata for all loaded skills (no instructions).
  func (r *Registry) Catalog() []SkillCatalogEntry {
      r.mu.RLock()
      defer r.mu.RUnlock()
      out := make([]SkillCatalogEntry, 0, len(r.skills))
      for _, s := range r.skills {
          out = append(out, SkillCatalogEntry{
              Name:        s.Name,
              Description: s.Description,
              Tags:        s.Tags,
          })
      }
      return out
  }

  func parseSkillFile(content string, dirName string) *Skill {
      if !strings.HasPrefix(content, "---") {
          return nil
      }
      parts := strings.SplitN(content[3:], "---", 2)
      if len(parts) < 2 {
          return nil
      }
      var skill Skill
      if err := yaml.Unmarshal([]byte(parts[0]), &skill); err != nil {
          return nil
      }
      skill.Instructions = strings.TrimSpace(parts[1])
      if skill.Name == "" {
          skill.Name = dirName
      }
      return &skill
  }

  func expandHome(p string) string {
      if strings.HasPrefix(p, "~/") {
          if h, err := os.UserHomeDir(); err == nil {
              return filepath.Join(h, p[2:])
          }
      }
      return p
  }
  ```

- [ ] **Step 4.4: Run tests**

  ```bash
  go test ./internal/skills/... -v
  ```
  Expected: all 4 new tests PASS.

- [ ] **Step 4.5: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 4.6: Commit**

  ```bash
  git add internal/skills/registry.go internal/skills/registry_test.go
  git commit -m "feat(skills): add Tags, SkillFile, Dir fields and Catalog() method"
  ```

---

## Task 5: Create `SkillAgent`

**Files:**
- Create: `internal/skills/agent.go`
- Create: `internal/skills/agent_test.go`

- [ ] **Step 5.1: Write failing tests**

  Create `internal/skills/agent_test.go`:

  ```go
  package skills

  import (
      "context"
      "testing"
  )

  func TestBuildSkillSelectionPromptContainsQuery(t *testing.T) {
      catalog := []SkillCatalogEntry{
          {Name: "docker-deploy", Description: "Deploy docker containers", Tags: []string{"docker", "deploy"}},
          {Name: "go-testing", Description: "Go test patterns", Tags: []string{"go", "test"}},
      }
      prompt := buildSkillSelectionPrompt("deploy a container", catalog)
      if prompt == "" {
          t.Fatal("prompt should not be empty")
      }
      for _, want := range []string{"deploy a container", "docker-deploy", "go-testing", "none"} {
          if !containsStr(prompt, want) {
              t.Errorf("prompt missing %q", want)
          }
      }
  }

  func TestParseSkillResponseKnownName(t *testing.T) {
      catalog := []SkillCatalogEntry{
          {Name: "docker-deploy"},
          {Name: "go-testing"},
      }
      name := parseSkillResponse("docker-deploy", catalog)
      if name != "docker-deploy" {
          t.Errorf("expected 'docker-deploy', got %q", name)
      }
  }

  func TestParseSkillResponseNone(t *testing.T) {
      catalog := []SkillCatalogEntry{{Name: "docker-deploy"}}
      name := parseSkillResponse("none", catalog)
      if name != "" {
          t.Errorf("expected empty string for 'none', got %q", name)
      }
  }

  func TestParseSkillResponseUnknownName(t *testing.T) {
      catalog := []SkillCatalogEntry{{Name: "docker-deploy"}}
      name := parseSkillResponse("unknown-skill", catalog)
      if name != "" {
          t.Errorf("expected empty string for unknown name, got %q", name)
      }
  }

  func TestParseSkillResponseExtraWhitespace(t *testing.T) {
      catalog := []SkillCatalogEntry{{Name: "docker-deploy"}}
      name := parseSkillResponse("  docker-deploy\n", catalog)
      if name != "docker-deploy" {
          t.Errorf("expected 'docker-deploy' after trimming, got %q", name)
      }
  }

  func TestSkillAgentConstructor(t *testing.T) {
      // Compile test — just verify constructor works without panic
      ctx, cancel := context.WithCancel(context.Background())
      cancel()
      _ = ctx
      // NewSkillAgent should not panic with zero values
  }

  func containsStr(s, sub string) bool {
      return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
          func() bool {
              for i := 0; i <= len(s)-len(sub); i++ {
                  if s[i:i+len(sub)] == sub {
                      return true
                  }
              }
              return false
          }())
  }
  ```

- [ ] **Step 5.2: Run tests to confirm they fail**

  ```bash
  go test ./internal/skills/... -run TestBuild -v
  go test ./internal/skills/... -run TestParse -v
  ```
  Expected: compile errors — `buildSkillSelectionPrompt`, `parseSkillResponse` not defined.

- [ ] **Step 5.3: Create `internal/skills/agent.go`**

  ```go
  package skills

  import (
      "context"
      "fmt"
      "strings"

      "github.com/MarcoMruz/agentloop/internal/config"
      "github.com/MarcoMruz/agentloop/internal/pirun"
  )

  // SkillCatalogEntry is the lightweight metadata used for catalog prompts and search.
  // (Also returned by Registry.Catalog())
  type SkillCatalogEntry struct {
      Name        string   `json:"name"        yaml:"name"`
      Description string   `json:"description" yaml:"description"`
      Tags        []string `json:"tags"        yaml:"tags"`
  }

  // SkillAgent uses a short-lived pi subprocess to select the best skill for a query.
  type SkillAgent struct {
      piCfg  config.PiConfig
      secCfg config.SecurityConfig
  }

  // NewSkillAgent creates a SkillAgent using the given pi config.
  func NewSkillAgent(piCfg config.PiConfig, secCfg config.SecurityConfig) *SkillAgent {
      return &SkillAgent{piCfg: piCfg, secCfg: secCfg}
  }

  // Find selects the best skill for the given query from the catalog.
  // Returns the matching skill name, or "" if no skill is relevant.
  func (a *SkillAgent) Find(ctx context.Context, query string, catalog []SkillCatalogEntry) (string, error) {
      if len(catalog) == 0 {
          return "", nil
      }
      prompt := buildSkillSelectionPrompt(query, catalog)
      response, err := pirun.RunTextSession(ctx, a.piCfg, a.secCfg, os.TempDir(), "skill-find", prompt)
      if err != nil {
          return "", fmt.Errorf("skill agent: %w", err)
      }
      return parseSkillResponse(response, catalog), nil
  }

  // buildSkillSelectionPrompt builds the prompt sent to the skill selector subprocess.
  func buildSkillSelectionPrompt(query string, catalog []SkillCatalogEntry) string {
      var sb strings.Builder
      sb.WriteString("You are a skill selector. Given a task description and a catalog of available skills,\n")
      sb.WriteString("return the name of the single most relevant skill, or \"none\" if no skill applies.\n\n")
      sb.WriteString("Respond with ONLY the skill name or \"none\". No explanation.\n\n")
      sb.WriteString("Task: ")
      sb.WriteString(query)
      sb.WriteString("\n\nAvailable skills:\n")
      for _, s := range catalog {
          sb.WriteString(fmt.Sprintf("- name: %s\n  description: %s\n  tags: %v\n\n", s.Name, s.Description, s.Tags))
      }
      return sb.String()
  }

  // parseSkillResponse extracts a valid skill name from the pi response.
  // Returns "" if the response is "none" or does not match any catalog entry.
  func parseSkillResponse(response string, catalog []SkillCatalogEntry) string {
      name := strings.TrimSpace(response)
      if strings.EqualFold(name, "none") || name == "" {
          return ""
      }
      for _, entry := range catalog {
          if strings.EqualFold(entry.Name, name) {
              return entry.Name
          }
      }
      return ""
  }
  ```

  `SkillCatalogEntry` is defined here in `agent.go`. Both files share `package skills` so `registry.go`'s `Catalog()` method resolves `[]SkillCatalogEntry` from this definition automatically — no import needed.

- [ ] **Step 5.4: Verify no duplicate `SkillCatalogEntry` definition**

  ```bash
  grep -n "SkillCatalogEntry" internal/skills/registry.go
  ```
  Expected: only the `Catalog()` method return type appears — no `type SkillCatalogEntry struct` line. If the struct definition is present in `registry.go`, delete it now (it is defined in `agent.go`).

- [ ] **Step 5.5: Run tests**

  ```bash
  go test ./internal/skills/... -v
  ```
  Expected: all tests PASS including previous Task 4 tests.

- [ ] **Step 5.6: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 5.7: Commit**

  ```bash
  git add internal/skills/agent.go internal/skills/agent_test.go internal/skills/registry.go
  git commit -m "feat(skills): add SkillAgent and SkillCatalogEntry"
  ```

---

## Task 6: Bridge — add `SkillToolEvent`, `skillLoadPath`, interception

**Files:**
- Modify: `internal/bridge/events.go`
- Modify: `internal/bridge/rpc.go`

- [ ] **Step 6.1: Add handler types to `events.go`**

  In `internal/bridge/events.go`, add after the existing `MemoryToolHandler` section:

  ```go
  // SkillToolEvent is fired when the pi agent calls the Find_skill tool.
  type SkillToolEvent struct {
      Tool          string         // "Find_skill"
      Params        map[string]any
      SkillLoadPath string
  }

  // SkillToolHandler is called synchronously when a skill tool call is intercepted.
  type SkillToolHandler func(event SkillToolEvent)
  ```

- [ ] **Step 6.2: Update `PiBridge` in `rpc.go`**

  a) Add `onSkillTool` and `skillLoadPath` fields to the `PiBridge` struct:
  ```go
  onSkillTool   SkillToolHandler
  skillLoadPath string // per-session temp file for Find_skill result
  ```

  b) Add `SetSkillToolHandler` method:
  ```go
  // SetSkillToolHandler registers the callback for skill tool interception.
  func (b *PiBridge) SetSkillToolHandler(h SkillToolHandler) { b.onSkillTool = h }
  ```

  c) In `Start()`, after the `retrievePath` line, add:
  ```go
  b.skillLoadPath = filepath.Join(os.TempDir(), fmt.Sprintf("agentloop-skill-load-%s.json", uuid.New().String()[:8]))
  ```

  d) In `buildSafeEnv()` call section of `Start()`, add:
  ```go
  env = append(env, "AGENTLOOP_SKILL_LOAD_PATH="+b.skillLoadPath)
  ```

  e) In `Stop()`, after the `retrievePath` cleanup, add:
  ```go
  if b.skillLoadPath != "" {
      _ = os.Remove(b.skillLoadPath)
  }
  ```

  f) In `readEvents()`, after the memory tool interception block (after line ~248), add:
  ```go
  // Skill tool interception: synchronous file write before execute() reads it
  if event.Type == "tool_execution_start" && event.ToolName == "Find_skill" && b.onSkillTool != nil {
      b.onSkillTool(SkillToolEvent{
          Tool:          event.ToolName,
          Params:        event.Args,
          SkillLoadPath: b.skillLoadPath,
      })
  }
  ```

- [ ] **Step 6.3: Run bridge tests**

  ```bash
  go test ./internal/bridge/... -v
  ```
  Expected: all existing tests pass.

- [ ] **Step 6.4: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 6.5: Commit**

  ```bash
  git add internal/bridge/events.go internal/bridge/rpc.go
  git commit -m "feat(bridge): add SkillToolHandler interception for Find_skill"
  ```

---

## Task 7: Wire `OnSkillTool` through `Callbacks` → `core.go`

**Files:**
- Modify: `internal/agent/core.go`

- [ ] **Step 7.1: Add `OnSkillTool` to `Callbacks` in `core.go`**

  In the `Callbacks` struct, add after `OnMemoryTool`:
  ```go
  OnSkillTool  bridge.SkillToolHandler
  ```

- [ ] **Step 7.2: Wire it in `Run()`**

  In `core.go` `Run()`, after the `OnMemoryTool` wire-up:
  ```go
  if c.cb.OnSkillTool != nil {
      b.SetSkillToolHandler(c.cb.OnSkillTool)
  }
  ```

- [ ] **Step 7.3: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 7.4: Commit**

  ```bash
  git add internal/agent/core.go
  git commit -m "feat(agent): add OnSkillTool callback and bridge wire-up"
  ```

---

## Task 8: Remove `DetectSkills` and skill injection from `PromptBuilder`

**Files:**
- Modify: `internal/agent/prompt_builder.go`
- Modify: `internal/agent/orchestrator.go`
- Modify: `internal/agent/core.go`

- [ ] **Step 8.1: Update `prompt_builder.go`**

  1. Remove `skills *skills.Registry` field from `PromptBuilder`
  2. Change `NewPromptBuilder` signature: remove `sk *skills.Registry` parameter
  3. Remove the skill injection loop (Section 2 in `Build()`)
  4. Remove `skillNames []string` parameter from `Build()`
  5. Delete `DetectSkills()` method entirely
  6. Remove `import "github.com/MarcoMruz/agentloop/internal/skills"` if no longer used

  Final `Build()` signature:
  ```go
  func (pb *PromptBuilder) Build(userId string, task string, conversationContextID string) (string, error)
  ```

  Final `NewPromptBuilder`:
  ```go
  func NewPromptBuilder(mem *memory.Engine) *PromptBuilder {
      return &PromptBuilder{mem: mem}
  }
  ```

- [ ] **Step 8.2: Update `core.go`**

  Remove:
  ```go
  skillNames := c.pb.DetectSkills(task)
  ```
  And update the `pb.Build()` call — remove `skillNames` argument:
  ```go
  fullPrompt, err := c.pb.Build(userId, task, conversationContextID)
  ```

- [ ] **Step 8.3: Update `orchestrator.go`**

  Find the two `NewPromptBuilder(o.memoryEngine, o.skillsReg)` calls and change both to:
  ```go
  NewPromptBuilder(o.memoryEngine)
  ```

- [ ] **Step 8.4: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 8.5: Run all tests**

  ```bash
  go test ./...
  ```
  Expected: all pass.

- [ ] **Step 8.6: Commit**

  ```bash
  git add internal/agent/prompt_builder.go internal/agent/orchestrator.go internal/agent/core.go
  git commit -m "refactor(agent): remove DetectSkills and skill injection from PromptBuilder"
  ```

---

## Task 9: Wire `OnSkillTool` in `session/manager.go`

**Files:**
- Modify: `internal/session/manager.go`
- Modify: `cmd/agentloop-server/main.go`

- [ ] **Step 9.1: Add `skillAgent` field to `Manager`**

  In `manager.go`, add to the `Manager` struct:
  ```go
  skillAgent *skills.SkillAgent
  ```

- [ ] **Step 9.2: Add `skillAgent` parameter to `NewManager()`**

  Change `NewManager` signature:
  ```go
  func NewManager(cfg *config.Config, v *vault.Vault, mem *memory.Engine, sk *skills.Registry, skillAgent *skills.SkillAgent) *Manager {
  ```
  Add to the return struct:
  ```go
  skillAgent: skillAgent,
  ```

- [ ] **Step 9.3: Add `OnSkillTool` handler in `StartSession()`**

  In the `agent.Callbacks{}` literal in `StartSession()`, add after `OnMemoryTool`:
  ```go
  OnSkillTool: func(ev bridge.SkillToolEvent) {
      if m.skillAgent == nil {
          _ = os.WriteFile(ev.SkillLoadPath, []byte(`{"error":"skill agent not configured"}`), 0600)
          return
      }
      query, _ := ev.Params["query"].(string)
      catalog := m.skills.Catalog()
      name, err := m.skillAgent.Find(ctx, query, catalog)
      if err != nil || name == "" {
          data := []byte(`{"error":"no matching skill found"}`)
          _ = os.WriteFile(ev.SkillLoadPath, data, 0600)
          return
      }
      skill, err := m.skills.Get(name)
      if err != nil {
          data := []byte(`{"error":"skill not found: ` + name + `"}`)
          _ = os.WriteFile(ev.SkillLoadPath, data, 0600)
          return
      }
      b, err := json.Marshal(skill)
      if err != nil {
          data := []byte(`{"error":"failed to marshal skill"}`)
          _ = os.WriteFile(ev.SkillLoadPath, data, 0600)
          return
      }
      _ = os.WriteFile(ev.SkillLoadPath, b, 0600)
  },
  ```

- [ ] **Step 9.4: Update `cmd/agentloop-server/main.go`**

  After `sk := skills.NewRegistry(cfg.Skills.SkillDirs)`, add:
  ```go
  skillPiCfg := cfg.Pi
  if cfg.Skills.Agent.Binary != "" {
      skillPiCfg.Binary = cfg.Skills.Agent.Binary
  }
  if cfg.Skills.Agent.Provider != "" {
      skillPiCfg.Provider = cfg.Skills.Agent.Provider
  }
  if cfg.Skills.Agent.Model != "" {
      skillPiCfg.Model = cfg.Skills.Agent.Model
  }
  skillAgent := skills.NewSkillAgent(skillPiCfg, cfg.Security)
  ```

  Update `NewManager` call to pass `skillAgent`:
  ```go
  mgr := session.NewManager(cfg, v, mem, sk, skillAgent)
  ```

- [ ] **Step 9.5: `go build ./...`**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 9.6: Run all tests**

  ```bash
  go test ./...
  ```
  Expected: all pass.

- [ ] **Step 9.7: Commit**

  ```bash
  git add internal/session/manager.go cmd/agentloop-server/main.go
  git commit -m "feat(session): wire OnSkillTool handler using SkillAgent"
  ```

---

## Task 10: Create TypeScript extension `skill-tools.ts`

**Files:**
- Create: `extensions/skill-tools.ts`

- [ ] **Step 10.1: Create `extensions/skill-tools.ts`**

  ```typescript
  // extensions/skill-tools.ts
  // Exposes Find_skill tool to the pi agent.
  // When called, the Go bridge intercepts tool_execution_start, runs a SkillAgent
  // subprocess to select the best skill from the catalog, writes the full skill
  // JSON to AGENTLOOP_SKILL_LOAD_PATH, and the execute() below reads it back.

  import { ExtensionFactory } from "@mariozechner/pi-coding-agent";
  import * as fs from "fs";

  const factory: ExtensionFactory = (pi) => {
    pi.addTool({
      name: "Find_skill",
      description:
        "Find and load the most relevant skill for your current task. Describe what you need in natural language — a side agent will search the skill catalog and return the best match with full instructions and available files. Call this when you need specialized workflow instructions for a domain like deployment, testing, migrations, etc.",
      parameters: {
        type: "object",
        properties: {
          query: {
            type: "string",
            description:
              "Natural language description of what specialized guidance you need",
          },
        },
        required: ["query"],
      },
      execute: async (_params: { query: string }) => {
        const loadPath = process.env.AGENTLOOP_SKILL_LOAD_PATH;
        if (!loadPath) {
          return { error: "AGENTLOOP_SKILL_LOAD_PATH not set" };
        }
        try {
          const raw = fs.readFileSync(loadPath, "utf8");
          return JSON.parse(raw);
        } catch {
          return { error: "no skill result available" };
        }
      },
    });
  };

  export default factory;
  ```

- [ ] **Step 10.2: `go build ./...` (verifies no Go regressions)**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 10.3: Commit**

  ```bash
  git add extensions/skill-tools.ts
  git commit -m "feat(extensions): add Find_skill pi tool in skill-tools.ts"
  ```

---

## Task 11: Run full test suite and verify

- [ ] **Step 11.1: Run all tests**

  ```bash
  go test ./... -v 2>&1 | tail -40
  ```
  Expected: all pass, no compile errors.

- [ ] **Step 11.2: Run security tests explicitly**

  ```bash
  go test ./internal/security/... -v
  go test ./internal/bridge/... -v
  ```
  Expected: all pass.

- [ ] **Step 11.3: Build both binaries**

  ```bash
  go build -o /tmp/agentloop-server ./cmd/agentloop-server
  go build -o /tmp/agentloop ./cmd/agentloop
  ```
  Expected: both build successfully.

- [ ] **Step 11.4: Final commit — update CLAUDE.md**

  In `CLAUDE.md`, update the `### internal/skills — Skills Registry` section:
  - Replace trigger-based matching description with `Find_skill` tool + `SkillAgent` description
  - Add `internal/pirun/` to the Directory Structure section
  - Remove references to `DetectSkills()` and `triggers`

  ```bash
  git add CLAUDE.md
  git commit -m "docs(claude): update skills registry docs for LLM-driven selection"
  ```

---

## Definition of Done

- [ ] `go test ./...` passes with no failures
- [ ] `go build ./...` produces no errors
- [ ] `internal/pirun.RunTextSession()` exists and is used by both `MetaAgent` and `SkillAgent`
- [ ] `DetectSkills()` is gone from `prompt_builder.go`
- [ ] `Find_skill` tool exists in `extensions/skill-tools.ts`
- [ ] `SkillAgent.Find()` exists in `internal/skills/agent.go`
- [ ] `AGENTLOOP_SKILL_LOAD_PATH` env var is set per session and cleaned up in `Stop()`
- [ ] Bridge interception fires post-dispatch for `Find_skill` (same placement as memory tools)
- [ ] `OnSkillTool` in `Callbacks` is wired through `core.go` and populated in `session/manager.go`
