package agent

import (
	"testing"

	"github.com/MarcoMruz/agentloop/internal/config"
)

func TestOrchestratorCtxIDGeneration(t *testing.T) {
	octx := NewOrchestratorCtx("sess-abc", "marco", "/tmp/proj", "cli", "", config.Defaults().Orchestrator)
	if octx.OrchestrationID == "" {
		t.Fatal("ID should be generated")
	}
	if octx.SessionID != "sess-abc" {
		t.Fatal("session ID mismatch")
	}
}

func TestBuildOutputSummarySingle(t *testing.T) {
	result := &OrchestratorResult{Mode: "single", SingleResult: &RunResult{Output: "done"}}
	if result.BuildOutputSummary() != "done" {
		t.Fatal("expected 'done'")
	}
}

func TestBuildOutputSummaryOrchestrated(t *testing.T) {
	result := &OrchestratorResult{
		OrchestrationID: "orch-test", Mode: "orchestrated",
		Iterations: []IterationResult{{Iteration: 1, Summaries: []WorkerSummary{{StepID: "s1", Status: "done", Summary: "wrote tests"}}, Verdict: &JudgeVerdict{Pass: true}}},
	}
	out := result.BuildOutputSummary()
	if out == "" {
		t.Fatal("output should not be empty")
	}
}

func TestCollectToolsUsed(t *testing.T) {
	result := &OrchestratorResult{
		Iterations: []IterationResult{{Summaries: []WorkerSummary{
			{ToolsUsed: []string{"write", "bash"}},
			{ToolsUsed: []string{"bash", "read"}},
		}}},
	}
	tools := result.CollectToolsUsed()
	if len(tools) != 3 {
		t.Fatalf("expected 3 unique tools, got %d", len(tools))
	}
}

func TestIterationResultAggregation(t *testing.T) {
	iter := IterationResult{
		Summaries: []WorkerSummary{
			{HITLDenials: 1, HITLApprovals: 2, TokensUsed: 5000},
			{HITLDenials: 0, HITLApprovals: 1, TokensUsed: 3000},
		},
	}
	totalDenials, totalApprovals, totalTokens := 0, 0, 0
	for _, s := range iter.Summaries {
		totalDenials += s.HITLDenials
		totalApprovals += s.HITLApprovals
		totalTokens += s.TokensUsed
	}
	if totalDenials != 1 {
		t.Fatalf("expected 1 denial, got %d", totalDenials)
	}
	if totalApprovals != 3 {
		t.Fatalf("expected 3, got %d", totalApprovals)
	}
	if totalTokens != 8000 {
		t.Fatalf("expected 8000, got %d", totalTokens)
	}
}
