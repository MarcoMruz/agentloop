package vault

import (
	"strings"
	"testing"
)

func TestParseFrontmatterValid(t *testing.T) {
	content := `---
id: sess-abc123
title: Fix tests
status: done
---
## Task
Fix the failing tests`

	yaml, body := ParseFrontmatter(content)
	if !strings.Contains(yaml, "id: sess-abc123") {
		t.Fatalf("expected yaml to contain id, got %q", yaml)
	}
	if !strings.Contains(yaml, "status: done") {
		t.Fatalf("expected yaml to contain status, got %q", yaml)
	}
	if !strings.Contains(body, "## Task") {
		t.Fatalf("expected body to contain task header, got %q", body)
	}
	if !strings.Contains(body, "Fix the failing tests") {
		t.Fatalf("expected body to contain task text, got %q", body)
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	content := `Just a plain markdown file
with no frontmatter at all.`

	yaml, body := ParseFrontmatter(content)
	if yaml != "" {
		t.Fatalf("expected empty yaml block, got %q", yaml)
	}
	if !strings.Contains(body, "Just a plain markdown file") {
		t.Fatalf("expected full content in body, got %q", body)
	}
}

func TestParseFrontmatterEmptyContent(t *testing.T) {
	yaml, body := ParseFrontmatter("")
	if yaml != "" {
		t.Fatalf("expected empty yaml, got %q", yaml)
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
}

func TestParseFrontmatterMultipleFields(t *testing.T) {
	content := `---
id: sess-12345678
title: Describe what this session does
created: 2025-01-15T10:30:00Z
updated: 2025-01-15T11:00:00Z
status: running
provider: anthropic
model: claude-sonnet-4-20250514
source: cli
user_id: marco
tags: [go, testing]
---
## Task
Run tests

## Transcript` + "\n```\nsome output\n```"

	yaml, body := ParseFrontmatter(content)

	expectedFields := []string{
		"id: sess-12345678",
		"title: Describe what this session does",
		"status: running",
		"provider: anthropic",
		"model: claude-sonnet-4-20250514",
		"source: cli",
		"user_id: marco",
	}
	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Fatalf("expected yaml to contain %q, got %q", field, yaml)
		}
	}
	if !strings.Contains(body, "## Task") {
		t.Fatalf("expected body to contain Task section, got %q", body)
	}
	if !strings.Contains(body, "## Transcript") {
		t.Fatalf("expected body to contain Transcript section, got %q", body)
	}
}

func TestParseFrontmatterOnlyDelimiters(t *testing.T) {
	content := "---\n---\nBody text here"
	yaml, body := ParseFrontmatter(content)
	if yaml != "" {
		t.Fatalf("expected empty yaml block for empty frontmatter, got %q", yaml)
	}
	if !strings.Contains(body, "Body text here") {
		t.Fatalf("expected body text, got %q", body)
	}
}

func TestParseFrontmatterUnclosedFrontmatter(t *testing.T) {
	content := "---\nid: sess-abc\ntitle: Unclosed"
	yaml, body := ParseFrontmatter(content)
	// With unclosed frontmatter, everything after first --- is treated as yaml
	if !strings.Contains(yaml, "id: sess-abc") {
		t.Fatalf("expected yaml content for unclosed frontmatter, got %q", yaml)
	}
	if body != "" {
		t.Fatalf("expected empty body for unclosed frontmatter, got %q", body)
	}
}

func TestParseFrontmatterPreservesBodyNewlines(t *testing.T) {
	content := "---\nid: test\n---\nLine 1\nLine 2\nLine 3"
	_, body := ParseFrontmatter(content)
	lines := strings.Split(body, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 body lines, got %d: %q", len(lines), body)
	}
}

func TestParseFrontmatterDashLineNotOnFirstLine(t *testing.T) {
	// When --- appears after other content, the parser still treats it as
	// frontmatter opener. Text before it goes to body, text after goes to yaml.
	content := "Some intro text\n---\nMore text"
	yaml, body := ParseFrontmatter(content)
	// "Some intro text" is before the ---, so it becomes body
	if !strings.Contains(body, "Some intro text") {
		t.Fatalf("expected body to contain intro text, got %q", body)
	}
	// "More text" is after the unclosed ---, so it becomes yaml
	if !strings.Contains(yaml, "More text") {
		t.Fatalf("expected yaml to contain text after ---, got %q", yaml)
	}
}
