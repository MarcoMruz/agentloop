package agent

import "testing"

func TestParseVerdictPass(t *testing.T) {
	text := `{"pass": true, "reasoning": "All criteria met", "gaps": [], "gap_specificity": 0, "iteration": 1}`

	verdict, err := ParseVerdict(text)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !verdict.Pass {
		t.Error("expected pass=true")
	}
	if verdict.Iteration != 1 {
		t.Errorf("expected iteration=1, got %d", verdict.Iteration)
	}
	if len(verdict.Gaps) != 0 {
		t.Errorf("expected 0 gaps, got %d", len(verdict.Gaps))
	}
}

func TestParseVerdictFail(t *testing.T) {
	text := `The judge has spoken:
{
  "pass": false,
  "reasoning": "Criterion not satisfied",
  "gaps": [
    {
      "criterion_id": "crit-1",
      "description": "Tests are not passing",
      "evidence": "go test output shows 3 failures",
      "suggested_fix": "Fix the failing assertions"
    }
  ],
  "gap_specificity": 2,
  "iteration": 2
}
End of verdict.`

	verdict, err := ParseVerdict(text)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if verdict.Pass {
		t.Error("expected pass=false")
	}
	if verdict.Iteration != 2 {
		t.Errorf("expected iteration=2, got %d", verdict.Iteration)
	}
	if len(verdict.Gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(verdict.Gaps))
	}
	gap := verdict.Gaps[0]
	if gap.CriterionID != "crit-1" {
		t.Errorf("expected criterion_id=crit-1, got %q", gap.CriterionID)
	}
	if gap.Description == "" {
		t.Error("expected non-empty gap description")
	}
	if gap.Evidence == "" {
		t.Error("expected non-empty gap evidence")
	}
	if gap.SuggestedFix == "" {
		t.Error("expected non-empty suggested fix")
	}
	if verdict.GapSpecificity != 2 {
		t.Errorf("expected gap_specificity=2, got %d", verdict.GapSpecificity)
	}
}

func TestParseVerdictInvalid(t *testing.T) {
	_, err := ParseVerdict("no json here whatsoever")
	if err == nil {
		t.Error("expected error for non-JSON text")
	}
}
