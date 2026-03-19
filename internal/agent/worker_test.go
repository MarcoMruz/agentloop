package agent

import (
	"strings"
	"testing"
	"time"
)

func TestTopoSortLinearChain(t *testing.T) {
	steps := []PlanStep{
		{ID: "s1", Dependencies: nil},
		{ID: "s2", Dependencies: []string{"s1"}},
		{ID: "s3", Dependencies: []string{"s2"}},
	}

	layers, err := TopoSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}

	if len(layers[0]) != 1 || layers[0][0].ID != "s1" {
		t.Errorf("layer 0 should contain only s1, got %v", layers[0])
	}
	if len(layers[1]) != 1 || layers[1][0].ID != "s2" {
		t.Errorf("layer 1 should contain only s2, got %v", layers[1])
	}
	if len(layers[2]) != 1 || layers[2][0].ID != "s3" {
		t.Errorf("layer 2 should contain only s3, got %v", layers[2])
	}
}

func TestTopoSortParallelSteps(t *testing.T) {
	steps := []PlanStep{
		{ID: "s1", Dependencies: nil},
		{ID: "s2", Dependencies: nil},
		{ID: "s3", Dependencies: []string{"s1", "s2"}},
	}

	layers, err := TopoSort(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}

	if len(layers[0]) != 2 {
		t.Errorf("layer 0 should contain 2 steps (s1, s2), got %d", len(layers[0]))
	}

	ids0 := map[string]bool{}
	for _, s := range layers[0] {
		ids0[s.ID] = true
	}
	if !ids0["s1"] || !ids0["s2"] {
		t.Errorf("layer 0 should contain s1 and s2, got %v", layers[0])
	}

	if len(layers[1]) != 1 || layers[1][0].ID != "s3" {
		t.Errorf("layer 1 should contain only s3, got %v", layers[1])
	}
}

func TestTopoSortCycleDetection(t *testing.T) {
	steps := []PlanStep{
		{ID: "s1", Dependencies: []string{"s2"}},
		{ID: "s2", Dependencies: []string{"s1"}},
	}

	_, err := TopoSort(steps)
	if err == nil {
		t.Fatal("expected error for cyclic dependency, got nil")
	}
}

func TestSummaryTruncation(t *testing.T) {
	// 4 chars per token, so 10 tokens = 40 chars
	long := strings.Repeat("a", 200)
	result := TruncateSummary(long, 10)

	// estimated tokens of result should be <= maxTokens
	estimated := estimateTokens(result)
	if estimated > 10 {
		t.Errorf("expected at most 10 estimated tokens, got %d", estimated)
	}

	// Short text should be returned as-is
	short := "hello world"
	result2 := TruncateSummary(short, 100)
	if result2 != short {
		t.Errorf("short text should be returned unchanged, got %q", result2)
	}
}

func TestWorkerSummaryFields(t *testing.T) {
	ws := WorkerSummary{
		StepID:        "step-1",
		Status:        "done",
		Summary:       "completed successfully",
		ToolsUsed:     []string{"bash", "edit"},
		HITLDenials:   1,
		HITLApprovals: 2,
		TokensUsed:    500,
		Duration:      3 * time.Second,
	}

	if ws.StepID != "step-1" {
		t.Errorf("StepID mismatch: %s", ws.StepID)
	}
	if ws.Status != "done" {
		t.Errorf("Status mismatch: %s", ws.Status)
	}
	if len(ws.ToolsUsed) != 2 {
		t.Errorf("expected 2 tools, got %d", len(ws.ToolsUsed))
	}
	if ws.HITLDenials != 1 {
		t.Errorf("HITLDenials mismatch: %d", ws.HITLDenials)
	}
	if ws.HITLApprovals != 2 {
		t.Errorf("HITLApprovals mismatch: %d", ws.HITLApprovals)
	}
	if ws.TokensUsed != 500 {
		t.Errorf("TokensUsed mismatch: %d", ws.TokensUsed)
	}
	if ws.Duration != 3*time.Second {
		t.Errorf("Duration mismatch: %v", ws.Duration)
	}
}
