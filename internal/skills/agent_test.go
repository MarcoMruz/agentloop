package skills

import (
	"context"
	"strings"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/config"
)

func TestParseSkillResponseMatch(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Build and deploy Docker containers", Tags: []string{"docker", "deployment"}},
		{Name: "go-testing", Description: "Go test patterns", Tags: []string{"go", "testing"}},
	}
	got := parseSkillResponse("docker-deploy", catalog)
	if got != "docker-deploy" {
		t.Errorf("expected docker-deploy, got %q", got)
	}
}

func TestParseSkillResponseNone(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Deploy Docker containers", Tags: []string{"docker"}},
	}
	got := parseSkillResponse("none", catalog)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestParseSkillResponseNoneUppercase(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Deploy Docker containers", Tags: []string{"docker"}},
	}
	got := parseSkillResponse("None", catalog)
	if got != "" {
		t.Errorf("expected empty string for 'None', got %q", got)
	}
}

func TestParseSkillResponseNoMatch(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Deploy Docker containers", Tags: []string{"docker"}},
	}
	got := parseSkillResponse("kubernetes-deploy", catalog)
	if got != "" {
		t.Errorf("expected empty string for unknown skill, got %q", got)
	}
}

func TestParseSkillResponseCaseInsensitive(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Deploy Docker containers", Tags: []string{"docker"}},
	}
	got := parseSkillResponse("DOCKER-DEPLOY", catalog)
	if got != "docker-deploy" {
		t.Errorf("expected docker-deploy from case-insensitive match, got %q", got)
	}
}

func TestParseSkillResponseWithExtraWhitespace(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Deploy Docker containers", Tags: []string{"docker"}},
	}
	got := parseSkillResponse("  docker-deploy  ", catalog)
	if got != "docker-deploy" {
		t.Errorf("expected docker-deploy with whitespace trimmed, got %q", got)
	}
}

func TestBuildSkillSelectionPrompt(t *testing.T) {
	catalog := []SkillCatalogEntry{
		{Name: "docker-deploy", Description: "Build and deploy Docker containers", Tags: []string{"docker", "deployment"}},
	}
	prompt := buildSkillSelectionPrompt("deploy a container", catalog)
	if !strings.Contains(prompt, "deploy a container") {
		t.Error("prompt should contain the query")
	}
	if !strings.Contains(prompt, "docker-deploy") {
		t.Error("prompt should contain skill name")
	}
	if !strings.Contains(prompt, "none") {
		t.Error("prompt should mention 'none' option")
	}
}

func TestSkillAgentFindEmptyCatalog(t *testing.T) {
	a := NewSkillAgent(config.PiConfig{}, config.SecurityConfig{})
	ctx := context.Background()
	name, err := a.Find(ctx, "deploy to production", nil)
	if err != nil {
		t.Errorf("expected no error for empty catalog, got: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty name for empty catalog, got %q", name)
	}
}
