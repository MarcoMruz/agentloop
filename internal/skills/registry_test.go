package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryCatalog(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: my-skill
description: A test skill
tags: [testing, go]
---
# Instructions
Do the thing.
`), 0644)
	reg := NewRegistry([]string{dir})
	catalog := reg.Catalog()
	if len(catalog) != 1 {
		t.Fatalf("expected 1 catalog entry, got %d", len(catalog))
	}
	if catalog[0].Name != "my-skill" {
		t.Errorf("expected name my-skill, got %q", catalog[0].Name)
	}
	if catalog[0].Description != "A test skill" {
		t.Errorf("expected description, got %q", catalog[0].Description)
	}
	if len(catalog[0].Tags) != 2 {
		t.Errorf("expected 2 tags, got %v", catalog[0].Tags)
	}
}

func TestSkillLoadWithFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "deploy-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: deploy-skill
description: Deployment skill
tags: [deploy]
files:
  - name: deploy.sh
    description: Runs the deployment pipeline
---
# Deploy instructions
`), 0644)
	os.WriteFile(filepath.Join(skillDir, "deploy.sh"), []byte("#!/bin/bash\necho deploy"), 0755)
	os.WriteFile(filepath.Join(skillDir, "Makefile"), []byte("deploy:\n\t./deploy.sh"), 0644)

	reg := NewRegistry([]string{dir})
	skill, err := reg.Get("deploy-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if skill.Dir != skillDir {
		t.Errorf("expected Dir %q, got %q", skillDir, skill.Dir)
	}

	// Check deploy.sh was found with description from manifest
	var deployFile *SkillFile
	var makefile *SkillFile
	for i := range skill.Files {
		if skill.Files[i].Name == "deploy.sh" {
			deployFile = &skill.Files[i]
		}
		if skill.Files[i].Name == "Makefile" {
			makefile = &skill.Files[i]
		}
	}
	if deployFile == nil {
		t.Fatal("deploy.sh not found in skill files")
	}
	if deployFile.Type != "sh" {
		t.Errorf("expected type sh, got %q", deployFile.Type)
	}
	if deployFile.Description != "Runs the deployment pipeline" {
		t.Errorf("expected description from manifest, got %q", deployFile.Description)
	}
	if deployFile.Path != filepath.Join(skillDir, "deploy.sh") {
		t.Errorf("expected absolute path, got %q", deployFile.Path)
	}
	if makefile == nil {
		t.Fatal("Makefile not found in skill files")
	}
	if makefile.Type != "" {
		t.Errorf("expected empty type for Makefile, got %q", makefile.Type)
	}
}

func TestSkillFileNoExtension(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: test\ntags: []\n---\n"), 0644)
	os.WriteFile(filepath.Join(skillDir, "Dockerfile"), []byte("FROM ubuntu"), 0644)

	reg := NewRegistry([]string{dir})
	skill, err := reg.Get("my-skill")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(skill.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(skill.Files))
	}
	if skill.Files[0].Name != "Dockerfile" {
		t.Errorf("expected Dockerfile, got %q", skill.Files[0].Name)
	}
	if skill.Files[0].Type != "" {
		t.Errorf("expected empty type, got %q", skill.Files[0].Type)
	}
}

func TestSkillTagsMigration(t *testing.T) {
	// Skills with old 'triggers' field parse without error — YAML ignores unknown fields
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "old-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: old-skill
description: Has old triggers field
triggers: [deploy, docker]
tags: [deployment, container]
---
# Old skill
`), 0644)
	reg := NewRegistry([]string{dir})
	skill, err := reg.Get("old-skill")
	if err != nil {
		t.Fatalf("expected no error with old triggers field, got: %v", err)
	}
	if len(skill.Tags) != 2 {
		t.Errorf("expected 2 tags, got %v", skill.Tags)
	}
}
