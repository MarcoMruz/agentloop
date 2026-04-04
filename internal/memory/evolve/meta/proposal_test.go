package meta

import (
	"strings"
	"testing"
)

func TestParseProposalEnhancedErrorMessages(t *testing.T) {
	// Test enhanced error messages
	_, err := ParseProposal("this has no json at all")
	if err == nil {
		t.Fatal("expected error for missing JSON")
	}
	if !strings.Contains(err.Error(), "no complete JSON object found") {
		t.Fatalf("expected enhanced error message, got: %v", err)
	}
}

func TestParseProposalTrailingComma(t *testing.T) {
	// Test trailing comma handling
	badJSON := `Here is my response:
{
  "reasoning": "test",
  "config_changes": null,
  "skill_changes": [],
  "agents_md_patch": "",
  "note_proposals": [],
  "summary": "test",
  "orchestrator_patches": [],
}
Done.`
	
	_, err := ParseProposal(badJSON)
	if err == nil {
		t.Fatal("expected error for trailing comma")
	}
	if !strings.Contains(err.Error(), "JSON parse error") {
		t.Fatalf("expected enhanced JSON error message, got: %v", err)
	}
}

func TestParseProposalDefaults(t *testing.T) {
	// Test that defaults are applied for missing fields
	minimalJSON := `{
  "config_changes": null,
  "skill_changes": [],
  "agents_md_patch": "",
  "note_proposals": [],
  "orchestrator_patches": []
}`
	
	proposal, err := ParseProposal("Response: " + minimalJSON)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	
	// Should have defaults applied
	if proposal.Summary == "" {
		t.Fatal("expected default summary")
	}
	if proposal.Reasoning == "" {
		t.Fatal("expected default reasoning")
	}
	
	// Should have non-nil slices
	if proposal.SkillChanges == nil {
		t.Fatal("expected non-nil SkillChanges slice")
	}
	if proposal.NoteProposals == nil {
		t.Fatal("expected non-nil NoteProposals slice")
	}
	if proposal.OrchestratorPatches == nil {
		t.Fatal("expected non-nil OrchestratorPatches slice")
	}
}

func TestParseProposalTruncatedError(t *testing.T) {
	// Test that very long JSON gets truncated in error messages
	longInvalidJSON := "Here's my response: {" + strings.Repeat(`"key": "value", `, 100) + "}"
	
	_, err := ParseProposal(longInvalidJSON)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	
	errorStr := err.Error()
	if !strings.Contains(errorStr, "...") {
		t.Fatal("expected truncated JSON in error message")
	}
	if len(errorStr) > 500 {
		t.Fatalf("error message too long (%d chars), expected truncation", len(errorStr))
	}
}