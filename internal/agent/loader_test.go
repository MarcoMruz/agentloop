package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAgentFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", filename, err)
	}
}

const plannerGuidelines = `---
name: planner-guidelines
role: planner
description: Planning guidelines
---

Break down tasks into steps.`

const plannerDefault = `---
name: planner-default
role: planner
description: Default planning instructions
---

Default: plan before acting.`

const plannerEvolved = `---
name: planner-evolved
role: planner
description: Evolved planning instructions
---

Evolved: adapt based on outcomes.`

// TestLoaderProjectAgentFound verifies that a project agent file in
// {workDir}/agents/ is discovered with Source: "project".
func TestLoaderProjectAgentFound(t *testing.T) {
	vaultDir := t.TempDir()
	workDir := t.TempDir()

	writeAgentFile(t, filepath.Join(workDir, "agents"), "planner-guidelines.md", plannerGuidelines)

	loader := NewAgentLoader(vaultDir)
	instructions := loader.Load("planner", workDir)

	if len(instructions) == 0 {
		t.Fatal("expected at least one instruction, got none")
	}

	var found *AgentInstruction
	for i := range instructions {
		if instructions[i].Name == "planner-guidelines" {
			found = &instructions[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected to find planner-guidelines instruction")
	}
	if found.Source != "project" {
		t.Errorf("expected Source %q, got %q", "project", found.Source)
	}
	if found.Role != "planner" {
		t.Errorf("expected Role %q, got %q", "planner", found.Role)
	}
}

// TestLoaderFallbackToDefault verifies that when no project agents dir exists,
// the loader falls back to {vaultDir}/agents/{role}-default.md with Source: "default".
func TestLoaderFallbackToDefault(t *testing.T) {
	vaultDir := t.TempDir()
	workDir := t.TempDir()
	// Note: no agents dir in workDir

	writeAgentFile(t, filepath.Join(vaultDir, "agents"), "planner-default.md", plannerDefault)

	loader := NewAgentLoader(vaultDir)
	instructions := loader.Load("planner", workDir)

	if len(instructions) == 0 {
		t.Fatal("expected at least one instruction, got none")
	}

	var found *AgentInstruction
	for i := range instructions {
		if instructions[i].Name == "planner-default" {
			found = &instructions[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected to find planner-default instruction")
	}
	if found.Source != "default" {
		t.Errorf("expected Source %q, got %q", "default", found.Source)
	}
}

// TestLoaderEvolvedAlwaysIncluded verifies that evolved defaults are always
// included alongside project agents when both exist.
func TestLoaderEvolvedAlwaysIncluded(t *testing.T) {
	vaultDir := t.TempDir()
	workDir := t.TempDir()

	writeAgentFile(t, filepath.Join(workDir, "agents"), "planner-guidelines.md", plannerGuidelines)
	writeAgentFile(t, filepath.Join(vaultDir, "agents"), "planner-evolved.md", plannerEvolved)

	loader := NewAgentLoader(vaultDir)
	instructions := loader.Load("planner", workDir)

	if len(instructions) < 2 {
		t.Fatalf("expected at least 2 instructions (project + evolved), got %d", len(instructions))
	}

	sources := map[string]bool{}
	for _, inst := range instructions {
		sources[inst.Source] = true
	}
	if !sources["project"] {
		t.Error("expected a project-sourced instruction")
	}
	if !sources["evolved"] {
		t.Error("expected an evolved-sourced instruction")
	}
}

// TestLoaderMultipleFilesAlphabetical verifies that multiple files for the same
// role are returned in alphabetical (filename) order.
func TestLoaderMultipleFilesAlphabetical(t *testing.T) {
	vaultDir := t.TempDir()
	workDir := t.TempDir()

	agentsDir := filepath.Join(workDir, "agents")
	writeAgentFile(t, agentsDir, "planner-zzz.md", `---
name: planner-zzz
role: planner
description: Last planner
---
ZZZ`)
	writeAgentFile(t, agentsDir, "planner-aaa.md", `---
name: planner-aaa
role: planner
description: First planner
---
AAA`)
	writeAgentFile(t, agentsDir, "planner-mmm.md", `---
name: planner-mmm
role: planner
description: Middle planner
---
MMM`)

	loader := NewAgentLoader(vaultDir)
	instructions := loader.Load("planner", workDir)

	// Filter to only project instructions
	var project []AgentInstruction
	for _, inst := range instructions {
		if inst.Source == "project" {
			project = append(project, inst)
		}
	}

	if len(project) != 3 {
		t.Fatalf("expected 3 project instructions, got %d", len(project))
	}
	if project[0].Name != "planner-aaa" {
		t.Errorf("expected first to be planner-aaa, got %s", project[0].Name)
	}
	if project[1].Name != "planner-mmm" {
		t.Errorf("expected second to be planner-mmm, got %s", project[1].Name)
	}
	if project[2].Name != "planner-zzz" {
		t.Errorf("expected third to be planner-zzz, got %s", project[2].Name)
	}
}

// TestLoaderInvalidFrontmatterSkipped verifies that files without valid
// YAML frontmatter delimiters are silently skipped.
func TestLoaderInvalidFrontmatterSkipped(t *testing.T) {
	vaultDir := t.TempDir()
	workDir := t.TempDir()

	agentsDir := filepath.Join(workDir, "agents")
	// Valid file
	writeAgentFile(t, agentsDir, "planner-valid.md", plannerGuidelines)
	// Invalid: no frontmatter at all
	writeAgentFile(t, agentsDir, "planner-invalid.md", "This file has no frontmatter delimiters.")

	loader := NewAgentLoader(vaultDir)
	instructions := loader.Load("planner", workDir)

	var project []AgentInstruction
	for _, inst := range instructions {
		if inst.Source == "project" {
			project = append(project, inst)
		}
	}

	if len(project) != 1 {
		t.Fatalf("expected exactly 1 valid project instruction, got %d", len(project))
	}
	if project[0].Name != "planner-guidelines" {
		t.Errorf("expected planner-guidelines, got %s", project[0].Name)
	}
}
