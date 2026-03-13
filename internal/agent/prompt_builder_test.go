package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/skills"
)

// writeSkill creates a minimal SKILL.md in a temp dir and returns the skill dir path.
func writeSkill(t *testing.T, root, name, description string, triggers []string, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	fm := "---\nname: " + name + "\n"
	if description != "" {
		fm += "description: \"" + description + "\"\n"
	}
	if len(triggers) > 0 {
		fm += "triggers:\n"
		for _, tr := range triggers {
			fm += "  - " + tr + "\n"
		}
	}
	fm += "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(fm), 0644); err != nil {
		t.Fatal(err)
	}
}

func newTestPromptBuilder(t *testing.T, skillDirs []string) *PromptBuilder {
	t.Helper()
	reg := skills.NewRegistry(skillDirs)
	// nil memory engine is fine — Build() is not called in these tests.
	return &PromptBuilder{skills: reg}
}

func TestDetectSkillsExplicitTriggers(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "my-skill", "some description", []string{"unittest", "foobar"}, "# body")

	pb := newTestPromptBuilder(t, []string{root})

	matched := pb.DetectSkills("please run unittest for me")
	if len(matched) != 1 || matched[0] != "my-skill" {
		t.Fatalf("expected [my-skill], got %v", matched)
	}
}

func TestDetectSkillsExplicitTriggersNoMatch(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "my-skill", "some description", []string{"unittest", "foobar"}, "# body")

	pb := newTestPromptBuilder(t, []string{root})

	matched := pb.DetectSkills("this has nothing to do with it")
	if len(matched) != 0 {
		t.Fatalf("expected no match, got %v", matched)
	}
}

func TestDetectSkillsDescriptionFallback(t *testing.T) {
	root := t.TempDir()
	// No triggers — only description. Simulates superpowers skills.
	writeSkill(t, root, "systematic-debugging", "Use when encountering any bug, test failure, or unexpected behavior", nil, "# body")

	pb := newTestPromptBuilder(t, []string{root})

	// "failure" is > 4 chars, not a stop word, and present in description and task.
	matched := pb.DetectSkills("there is a test failure in the auth module")
	if len(matched) != 1 || matched[0] != "systematic-debugging" {
		t.Fatalf("expected [systematic-debugging], got %v", matched)
	}
}

func TestDetectSkillsDescriptionFallbackNoMatch(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "systematic-debugging", "Use when encountering any bug, test failure, or unexpected behavior", nil, "# body")

	pb := newTestPromptBuilder(t, []string{root})

	// Task is completely unrelated.
	matched := pb.DetectSkills("please send an email to the team")
	if len(matched) != 0 {
		t.Fatalf("expected no match, got %v", matched)
	}
}

func TestDetectSkillsDescriptionFallbackBrainstorming(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "brainstorming",
		"You MUST use this before any creative work - creating features, building components, adding functionality, or modifying behavior",
		nil, "# body")

	pb := newTestPromptBuilder(t, []string{root})

	matched := pb.DetectSkills("I want to create a new feature for the user dashboard")
	if len(matched) != 1 || matched[0] != "brainstorming" {
		t.Fatalf("expected [brainstorming], got %v", matched)
	}
}

func TestDetectSkillsTriggersTakePrecedenceOverDescription(t *testing.T) {
	root := t.TempDir()
	// Skill has both triggers AND description. Triggers should match first.
	writeSkill(t, root, "dual-skill", "Use when doing something completely unrelated", []string{"exactmatch"}, "# body")

	pb := newTestPromptBuilder(t, []string{root})

	// Trigger keyword present — should match.
	matched := pb.DetectSkills("please exactmatch this")
	if len(matched) != 1 || matched[0] != "dual-skill" {
		t.Fatalf("expected [dual-skill], got %v", matched)
	}
}

func TestIsStopWord(t *testing.T) {
	stopWords := []string{"about", "being", "could", "their", "would", "which"}
	for _, w := range stopWords {
		if !isStopWord(w) {
			t.Errorf("expected %q to be a stop word", w)
		}
	}

	notStopWords := []string{"debug", "feature", "failure", "implement", "review"}
	for _, w := range notStopWords {
		if isStopWord(w) {
			t.Errorf("expected %q NOT to be a stop word", w)
		}
	}
}
