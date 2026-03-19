package agent

import (
	"time"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
)

// OrchestratorOutcome captures the aggregate result of a multi-iteration orchestration run.
type OrchestratorOutcome struct {
	OrchestrationID        string        `json:"orchestration_id"`
	UserID                 string        `json:"user_id"`
	Task                   string        `json:"task"`
	Iterations             int           `json:"iterations"`
	FinalPass              bool          `json:"final_pass"`
	TotalSteers            int           `json:"total_steers"`
	TotalHITLDenials       int           `json:"total_hitl_denials"`
	TotalHITLApprovals     int           `json:"total_hitl_approvals"`
	PlanStepCount          int           `json:"plan_step_count"`
	ActualStepsNeeded      int           `json:"actual_steps_needed"`
	MaxWorkerSummaryTokens int           `json:"max_worker_summary_tokens"`
	JudgeGapSpecificity    int           `json:"judge_gap_specificity"`
	EvolutionCount         int           `json:"evolution_count"`
	Keywords               []string      `json:"keywords"`
	Topics                 []string      `json:"topics"`
	PipelineID             string        `json:"pipeline_id"`
	Duration               time.Duration `json:"duration"`
}

// ToTaskOutcome converts an OrchestratorOutcome to a TaskOutcome for the existing Collector.
func (o *OrchestratorOutcome) ToTaskOutcome() metrics.TaskOutcome {
	finalStatus := "done"
	if !o.FinalPass {
		finalStatus = "error"
	}

	return metrics.TaskOutcome{
		SessionID:     o.OrchestrationID,
		UserID:        o.UserID,
		Timestamp:     time.Now(),
		HITLDenials:   o.TotalHITLDenials,
		HITLApprovals: o.TotalHITLApprovals,
		SteerCount:    o.TotalSteers,
		FinalStatus:   finalStatus,
		TokensUsed:    0,
		ToolCalls:     0,
		Duration:      o.Duration,
		TaskKeywords:  o.Keywords,
		TaskTopics:    o.Topics,
		PipelineID:    o.PipelineID,
	}
}
