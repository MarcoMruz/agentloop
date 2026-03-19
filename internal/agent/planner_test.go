package agent

import "testing"

func TestParsePlanValid(t *testing.T) {
	text := `Here is the plan:
{
  "mode": "orchestrate",
  "steps": [
    {
      "id": "step-1",
      "description": "Analyze the codebase",
      "dependencies": [],
      "worker_hint": "Use grep and read tools"
    },
    {
      "id": "step-2",
      "description": "Fix the bug",
      "dependencies": ["step-1"],
      "worker_hint": "Edit the relevant file"
    }
  ],
  "success_criteria": ["Tests pass", "No regressions"],
  "reasoning": "The task requires multiple steps"
}
Good luck!`

	plan, err := ParsePlan(text)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if plan.Mode != "orchestrate" {
		t.Errorf("expected mode=orchestrate, got %q", plan.Mode)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[0].ID != "step-1" {
		t.Errorf("expected step[0].ID=step-1, got %q", plan.Steps[0].ID)
	}
	if plan.Steps[1].ID != "step-2" {
		t.Errorf("expected step[1].ID=step-2, got %q", plan.Steps[1].ID)
	}
	if len(plan.Steps[1].Dependencies) != 1 || plan.Steps[1].Dependencies[0] != "step-1" {
		t.Errorf("expected step[1].Dependencies=[step-1], got %v", plan.Steps[1].Dependencies)
	}
	if len(plan.SuccessCriteria) != 2 {
		t.Errorf("expected 2 success criteria, got %d", len(plan.SuccessCriteria))
	}
	if plan.Reasoning == "" {
		t.Error("expected non-empty reasoning")
	}
}

func TestParsePlanSingleAgent(t *testing.T) {
	text := `{"mode": "single", "steps": [], "success_criteria": [], "reasoning": "Simple task"}`

	plan, err := ParsePlan(text)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if plan.Mode != "single" {
		t.Errorf("expected mode=single, got %q", plan.Mode)
	}
	if len(plan.Steps) != 0 {
		t.Errorf("expected 0 steps, got %d", len(plan.Steps))
	}
	if len(plan.SuccessCriteria) != 0 {
		t.Errorf("expected 0 success criteria, got %d", len(plan.SuccessCriteria))
	}
}

func TestParsePlanInvalid(t *testing.T) {
	_, err := ParsePlan("this is not JSON at all")
	if err == nil {
		t.Error("expected error for non-JSON text")
	}

	_, err = ParsePlan("")
	if err == nil {
		t.Error("expected error for empty string")
	}
}
