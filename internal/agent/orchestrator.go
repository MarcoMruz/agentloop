package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MarcoMruz/agentloop/internal/bridge"
	"github.com/MarcoMruz/agentloop/internal/config"
	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/metrics"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/meta"
	"github.com/MarcoMruz/agentloop/internal/skills"
	"github.com/google/uuid"
)

// OrchestratorCtx carries context for a single orchestration run.
type OrchestratorCtx struct {
	OrchestrationID string
	SessionID       string
	UserID          string
	WorkDir         string
	Source          string
	ConversationID  string
	StartedAt       time.Time
	Config          config.OrchestratorConfig
}

// NewOrchestratorCtx creates a new OrchestratorCtx with a generated OrchestrationID.
func NewOrchestratorCtx(sessionID, userID, workDir, source, conversationID string, cfg config.OrchestratorConfig) OrchestratorCtx {
	return OrchestratorCtx{
		OrchestrationID: "orch-" + uuid.New().String()[:8],
		SessionID:       sessionID,
		UserID:          userID,
		WorkDir:         workDir,
		Source:          source,
		ConversationID:  conversationID,
		StartedAt:       time.Now(),
		Config:          cfg,
	}
}

// OrchestratorResult captures the full outcome of an orchestration run.
type OrchestratorResult struct {
	OrchestrationID string
	Mode            string // "single" or "orchestrated"
	Plan            *Plan
	Iterations      []IterationResult
	FinalVerdict    *JudgeVerdict
	SingleResult    *RunResult
	TotalTokens     int
	TotalDuration   time.Duration
}

// IterationResult captures one iteration of the Planner-Worker-Judge loop.
type IterationResult struct {
	Iteration int
	Summaries []WorkerSummary
	Verdict   *JudgeVerdict
	Evolved   bool
}

// BuildOutputSummary formats a human-readable summary of the orchestration result.
func (r *OrchestratorResult) BuildOutputSummary() string {
	if r.Mode == "single" && r.SingleResult != nil {
		return r.SingleResult.Output
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Orchestration %s completed (%d iterations)\n", r.OrchestrationID, len(r.Iterations)))

	for _, iter := range r.Iterations {
		sb.WriteString(fmt.Sprintf("\n--- Iteration %d ---\n", iter.Iteration))
		for _, s := range iter.Summaries {
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s\n", s.Status, s.StepID, s.Summary))
		}
		if iter.Verdict != nil {
			if iter.Verdict.Pass {
				sb.WriteString("  Verdict: PASS\n")
			} else {
				sb.WriteString(fmt.Sprintf("  Verdict: FAIL (%d gaps)\n", len(iter.Verdict.Gaps)))
			}
		}
		if iter.Evolved {
			sb.WriteString("  [evolved pipeline]\n")
		}
	}

	if r.FinalVerdict != nil && r.FinalVerdict.Pass {
		sb.WriteString("\nFinal: ALL CRITERIA MET\n")
	} else if r.FinalVerdict != nil {
		sb.WriteString(fmt.Sprintf("\nFinal: %d gaps remaining\n", len(r.FinalVerdict.Gaps)))
	}

	return sb.String()
}

// CollectToolsUsed deduplicates all tools used across all iterations.
func (r *OrchestratorResult) CollectToolsUsed() []string {
	seen := map[string]bool{}
	var out []string
	for _, iter := range r.Iterations {
		for _, s := range iter.Summaries {
			for _, t := range s.ToolsUsed {
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
		}
	}
	return out
}

// Orchestrator implements the Planner-Worker-Judge loop with MemEvolve integration.
type Orchestrator struct {
	loader       *AgentLoader
	memoryEngine *memory.Engine
	skillsReg    *skills.Registry
	pipeline     *evolve.PipelineHolder
	collector    *metrics.Collector
	metaAgent    *meta.MetaAgent
	secCfg       config.SecurityConfig
	hitlCfg      config.HITLConfig
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(
	loader *AgentLoader,
	memEngine *memory.Engine,
	skillsReg *skills.Registry,
	pipeline *evolve.PipelineHolder,
	collector *metrics.Collector,
	metaAgent *meta.MetaAgent,
	secCfg config.SecurityConfig,
	hitlCfg config.HITLConfig,
) *Orchestrator {
	return &Orchestrator{
		loader:       loader,
		memoryEngine: memEngine,
		skillsReg:    skillsReg,
		pipeline:     pipeline,
		collector:    collector,
		metaAgent:    metaAgent,
		secCfg:       secCfg,
		hitlCfg:      hitlCfg,
	}
}

// Run executes the orchestration: plan, dispatch workers, judge, iterate.
func (o *Orchestrator) Run(ctx context.Context, octx OrchestratorCtx, task string, sess SessionInterface, cb Callbacks) *OrchestratorResult {
	start := time.Now()
	slog.Info("orchestrator starting", "orchestration_id", octx.OrchestrationID, "task_len", len(task))

	// Load planner instructions
	plannerInstructions := o.loader.Load("planner", octx.WorkDir)

	// Get memory context
	memCtx := o.getMemoryContext(octx.UserID, task, octx.ConversationID)

	// Run planner
	plan, err := o.runPlanner(ctx, octx, task, memCtx, plannerInstructions, nil, nil)
	if err != nil {
		slog.Error("planner failed", "error", err)
		if cb.OnError != nil {
			cb.OnError(fmt.Sprintf("planner failed: %v", err))
		}
		return &OrchestratorResult{
			OrchestrationID: octx.OrchestrationID,
			Mode:            "single",
			TotalDuration:   time.Since(start),
		}
	}

	// Single mode: delegate to Core.Run
	if plan.Mode == "single" {
		slog.Info("planner chose single mode", "orchestration_id", octx.OrchestrationID)
		pb := NewPromptBuilder(o.memoryEngine, o.skillsReg)
		core := New(octx.Config.Worker, o.secCfg, o.hitlCfg, pb, cb)
		result := core.Run(ctx, octx.UserID, task, octx.WorkDir, sess)
		return &OrchestratorResult{
			OrchestrationID: octx.OrchestrationID,
			Mode:            "single",
			Plan:            plan,
			SingleResult:    &result,
			TotalTokens:     result.Stats.Tokens,
			TotalDuration:   time.Since(start),
		}
	}

	// Orchestration loop
	slog.Info("planner chose orchestrate mode", "orchestration_id", octx.OrchestrationID, "steps", len(plan.Steps))

	var iterations []IterationResult
	var finalVerdict *JudgeVerdict
	evolutionCount := 0
	judgeInstructions := o.loader.Load("judge", octx.WorkDir)

	for iteration := 1; iteration <= octx.Config.MaxIterations; iteration++ {
		slog.Info("iteration starting", "orchestration_id", octx.OrchestrationID, "iteration", iteration)

		// Dispatch workers concurrently
		allSummaries := o.dispatchWorkers(ctx, octx, plan, memCtx, sess, cb)

		// Run judge
		verdict := o.runJudge(ctx, octx, task, plan, allSummaries, judgeInstructions, iteration)

		evolved := false
		iterations = append(iterations, IterationResult{
			Iteration: iteration,
			Summaries: allSummaries,
			Verdict:   verdict,
			Evolved:   evolved,
		})

		if verdict != nil && verdict.Pass {
			slog.Info("judge passed", "orchestration_id", octx.OrchestrationID, "iteration", iteration)
			finalVerdict = verdict
			break
		}

		// Mid-loop evolution: persist outcome before calling Evolve
		if o.metaAgent != nil && verdict != nil && !verdict.Pass && iteration < octx.Config.MaxIterations {
			outcome := o.buildOutcome(octx, task, iteration, false, allSummaries, verdict, evolutionCount)
			taskOutcome := outcome.ToTaskOutcome()
			if o.collector != nil {
				o.collector.Record(taskOutcome) // persist first!
			}
			o.metaAgent.Evolve(taskOutcome)
			evolutionCount++
			iterations[len(iterations)-1].Evolved = true

			// Replan with judge feedback
			plan, err = o.runPlanner(ctx, octx, task, memCtx, plannerInstructions, verdict, allSummaries)
			if err != nil {
				slog.Error("replanning failed", "error", err)
				finalVerdict = verdict
				break
			}
		}

		finalVerdict = verdict

		if iteration == octx.Config.MaxIterations {
			slog.Warn("max iterations reached", "orchestration_id", octx.OrchestrationID)
		}
	}

	// Record final outcome
	var allSummaries []WorkerSummary
	for _, iter := range iterations {
		allSummaries = append(allSummaries, iter.Summaries...)
	}

	finalPass := finalVerdict != nil && finalVerdict.Pass
	outcome := o.buildOutcome(octx, task, len(iterations), finalPass, allSummaries, finalVerdict, evolutionCount)
	taskOutcome := outcome.ToTaskOutcome()
	if o.collector != nil {
		o.collector.Record(taskOutcome)
	}

	result := &OrchestratorResult{
		OrchestrationID: octx.OrchestrationID,
		Mode:            "orchestrated",
		Plan:            plan,
		Iterations:      iterations,
		FinalVerdict:    finalVerdict,
		TotalTokens:     sumTokens(allSummaries),
		TotalDuration:   time.Since(start),
	}

	if cb.OnDone != nil {
		cb.OnDone(result.BuildOutputSummary(), RunStats{
			Tokens:   result.TotalTokens,
			Duration: result.TotalDuration,
		})
	}

	return result
}

// getMemoryContext retrieves the memory context for the orchestration.
func (o *Orchestrator) getMemoryContext(userID, task, conversationCtxID string) string {
	if o.memoryEngine == nil {
		return ""
	}
	var memCtx string
	var err error
	if conversationCtxID != "" {
		memCtx, err = o.memoryEngine.GetContextForUserAndConversationContext(userID, conversationCtxID)
	} else {
		memCtx, err = o.memoryEngine.GetContextForUserWithTask(userID, task)
	}
	if err != nil {
		slog.Warn("orchestrator: failed to get memory context", "error", err)
		return ""
	}
	return memCtx
}

// runPlanner runs a read-only pi session to generate a plan.
func (o *Orchestrator) runPlanner(ctx context.Context, octx OrchestratorCtx, task, memCtx string, instructions []AgentInstruction, prevVerdict *JudgeVerdict, prevSummaries []WorkerSummary) (*Plan, error) {
	prompt := o.buildPlannerPrompt(task, memCtx, instructions, prevVerdict, prevSummaries)
	response, err := o.runReadOnlyPi(ctx, octx.Config.Planner, octx.WorkDir, prompt)
	if err != nil {
		return nil, fmt.Errorf("planner pi session: %w", err)
	}
	plan, err := ParsePlan(response)
	if err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}
	slog.Info("plan parsed", "mode", plan.Mode, "steps", len(plan.Steps))
	return plan, nil
}

// runJudge runs a read-only pi session to evaluate worker output against success criteria.
func (o *Orchestrator) runJudge(ctx context.Context, octx OrchestratorCtx, task string, plan *Plan, summaries []WorkerSummary, instructions []AgentInstruction, iteration int) *JudgeVerdict {
	prompt := o.buildJudgePrompt(task, plan, summaries, instructions, iteration)
	response, err := o.runReadOnlyPi(ctx, octx.Config.Judge, octx.WorkDir, prompt)
	if err != nil {
		slog.Error("judge pi session failed", "error", err)
		return &JudgeVerdict{Pass: false, Reasoning: fmt.Sprintf("judge failed: %v", err), Iteration: iteration}
	}
	verdict, err := ParseVerdict(response)
	if err != nil {
		slog.Error("judge verdict parse failed", "error", err)
		return &JudgeVerdict{Pass: false, Reasoning: fmt.Sprintf("verdict parse error: %v", err), Iteration: iteration}
	}
	verdict.Iteration = iteration
	return verdict
}

// dispatchWorkers runs plan steps concurrently, bounded by pool size, respecting dependencies.
func (o *Orchestrator) dispatchWorkers(ctx context.Context, octx OrchestratorCtx, plan *Plan, memCtx string, sess SessionInterface, cb Callbacks) []WorkerSummary {
	layers, err := TopoSort(plan.Steps)
	if err != nil {
		slog.Error("topo sort failed", "error", err)
		return nil
	}

	var allSummaries []WorkerSummary
	workerInstructions := o.loader.Load("worker", octx.WorkDir)

	for _, layer := range layers {
		sem := make(chan struct{}, octx.Config.WorkerPoolSize)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, step := range layer {
			wg.Add(1)
			sem <- struct{}{}
			go func(s PlanStep) {
				defer wg.Done()
				defer func() { <-sem }()
				summary := o.runWorker(ctx, octx, s, memCtx, workerInstructions, sess, cb)
				mu.Lock()
				allSummaries = append(allSummaries, summary)
				mu.Unlock()
			}(step)
		}
		wg.Wait()
	}
	return allSummaries
}

// runWorker executes a single plan step using a Core instance.
func (o *Orchestrator) runWorker(ctx context.Context, octx OrchestratorCtx, step PlanStep, memCtx string, instructions []AgentInstruction, sess SessionInterface, cb Callbacks) WorkerSummary {
	start := time.Now()
	slog.Info("worker starting", "step_id", step.ID, "description", step.Description)

	prompt := o.buildWorkerPrompt(step, memCtx, instructions)

	// Create a worker-specific callback that prefixes errors with step ID
	workerCb := Callbacks{
		OnText:        cb.OnText,
		OnToolUse:     cb.OnToolUse,
		OnToolResult:  cb.OnToolResult,
		OnHITLRequest: cb.OnHITLRequest,
		OnDone:        nil, // Don't call OnDone per-worker; orchestrator handles final OnDone
		OnError: func(msg string) {
			if cb.OnError != nil {
				cb.OnError(fmt.Sprintf("[%s] %s", step.ID, msg))
			}
		},
		OnMemoryTool: cb.OnMemoryTool,
	}

	pb := NewPromptBuilder(o.memoryEngine, o.skillsReg)
	core := New(octx.Config.Worker, o.secCfg, o.hitlCfg, pb, workerCb)
	result := core.Run(ctx, octx.UserID, prompt, octx.WorkDir, sess)

	status := "done"
	if result.Error != "" {
		status = "failed"
	}

	summary := TruncateSummary(result.Output, octx.Config.SummaryMaxTokens)

	return WorkerSummary{
		StepID:    step.ID,
		Status:    status,
		Summary:   summary,
		ToolsUsed: result.ToolsUsed,
		TokensUsed: result.Stats.Tokens,
		Duration:  time.Since(start),
	}
}

// runReadOnlyPi runs a read-only pi session and returns the text response.
func (o *Orchestrator) runReadOnlyPi(ctx context.Context, piCfg config.PiConfig, workDir, prompt string) (string, error) {
	b := bridge.New(piCfg, o.secCfg, config.HITLConfig{})
	var response strings.Builder
	b.SetEventHandler(func(event bridge.RPCEvent) error {
		if event.Type == "message_update" && event.AssistantMessageEvent != nil {
			if event.AssistantMessageEvent.Type == "text_delta" {
				response.WriteString(event.AssistantMessageEvent.Delta)
			}
		}
		return nil
	})
	if err := b.Start(ctx, workDir); err != nil {
		return "", fmt.Errorf("start pi: %w", err)
	}
	defer b.Stop()
	if err := b.Prompt(ctx, "orchestrator", prompt); err != nil {
		return "", fmt.Errorf("prompt pi: %w", err)
	}
	<-b.Done()
	return response.String(), nil
}

// buildPlannerPrompt constructs the planner prompt from task, memory, instructions, and optional prior feedback.
func (o *Orchestrator) buildPlannerPrompt(task, memCtx string, instructions []AgentInstruction, prevVerdict *JudgeVerdict, prevSummaries []WorkerSummary) string {
	var sb strings.Builder

	sb.WriteString("You are a Planner agent. Analyze the task and produce a plan.\n\n")

	// Include loaded instructions
	for _, inst := range instructions {
		sb.WriteString(fmt.Sprintf("<instructions source=%q>\n%s\n</instructions>\n\n", inst.Source, inst.Content))
	}

	if memCtx != "" {
		sb.WriteString(fmt.Sprintf("<memory>\n%s\n</memory>\n\n", memCtx))
	}

	sb.WriteString(fmt.Sprintf("<task>\n%s\n</task>\n\n", task))

	// If replanning after a failed iteration, include judge feedback
	if prevVerdict != nil && !prevVerdict.Pass {
		sb.WriteString("<previous_verdict>\n")
		sb.WriteString(fmt.Sprintf("Pass: %v\nReasoning: %s\n", prevVerdict.Pass, prevVerdict.Reasoning))
		for _, gap := range prevVerdict.Gaps {
			sb.WriteString(fmt.Sprintf("Gap [%s]: %s\n  Suggested fix: %s\n", gap.CriterionID, gap.Description, gap.SuggestedFix))
		}
		sb.WriteString("</previous_verdict>\n\n")
	}

	if len(prevSummaries) > 0 {
		sb.WriteString("<previous_worker_output>\n")
		for _, s := range prevSummaries {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", s.Status, s.StepID, s.Summary))
		}
		sb.WriteString("</previous_worker_output>\n\n")
	}

	sb.WriteString(`Respond with a JSON object:
{
  "mode": "single" or "orchestrate",
  "steps": [{"id": "...", "description": "...", "dependencies": [...], "worker_hint": "..."}],
  "success_criteria": ["..."],
  "reasoning": "..."
}

Use "single" mode if the task is simple enough for one agent. Use "orchestrate" for complex multi-step tasks.`)

	return sb.String()
}

// buildWorkerPrompt constructs the prompt for a single worker step.
func (o *Orchestrator) buildWorkerPrompt(step PlanStep, memCtx string, instructions []AgentInstruction) string {
	var sb strings.Builder

	sb.WriteString("You are a Worker agent executing a specific step.\n\n")

	for _, inst := range instructions {
		sb.WriteString(fmt.Sprintf("<instructions source=%q>\n%s\n</instructions>\n\n", inst.Source, inst.Content))
	}

	if memCtx != "" {
		sb.WriteString(fmt.Sprintf("<memory>\n%s\n</memory>\n\n", memCtx))
	}

	sb.WriteString(fmt.Sprintf("<step id=%q>\n%s\n</step>\n", step.ID, step.Description))

	if step.WorkerHint != "" {
		sb.WriteString(fmt.Sprintf("\nHint: %s\n", step.WorkerHint))
	}

	sb.WriteString("\nComplete this step. Be thorough and report what you did.")

	return sb.String()
}

// buildJudgePrompt constructs the judge prompt from task, plan, summaries, and iteration info.
func (o *Orchestrator) buildJudgePrompt(task string, plan *Plan, summaries []WorkerSummary, instructions []AgentInstruction, iteration int) string {
	var sb strings.Builder

	sb.WriteString("You are a Judge agent. Evaluate whether the workers met the success criteria.\n\n")

	for _, inst := range instructions {
		sb.WriteString(fmt.Sprintf("<instructions source=%q>\n%s\n</instructions>\n\n", inst.Source, inst.Content))
	}

	sb.WriteString(fmt.Sprintf("<task>\n%s\n</task>\n\n", task))

	sb.WriteString("<success_criteria>\n")
	for i, c := range plan.SuccessCriteria {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
	}
	sb.WriteString("</success_criteria>\n\n")

	sb.WriteString("<worker_output>\n")
	for _, s := range summaries {
		sb.WriteString(fmt.Sprintf("[%s] Step %s: %s\n", s.Status, s.StepID, s.Summary))
		if len(s.ToolsUsed) > 0 {
			sb.WriteString(fmt.Sprintf("  Tools: %s\n", strings.Join(s.ToolsUsed, ", ")))
		}
	}
	sb.WriteString("</worker_output>\n\n")

	sb.WriteString(fmt.Sprintf("Iteration: %d\n\n", iteration))

	sb.WriteString(`Respond with a JSON object:
{
  "pass": true/false,
  "reasoning": "...",
  "gaps": [{"criterion_id": "...", "description": "...", "evidence": "...", "suggested_fix": "..."}],
  "gap_specificity": 0-10
}`)

	return sb.String()
}

// buildOutcome constructs an OrchestratorOutcome from the run state.
func (o *Orchestrator) buildOutcome(octx OrchestratorCtx, task string, iterations int, finalPass bool, summaries []WorkerSummary, verdict *JudgeVerdict, evolCount int) *OrchestratorOutcome {
	totalDenials, totalApprovals, maxTokens, totalTokens, totalTools := 0, 0, 0, 0, 0
	for _, s := range summaries {
		totalDenials += s.HITLDenials
		totalApprovals += s.HITLApprovals
		totalTokens += s.TokensUsed
		totalTools += len(s.ToolsUsed)
		if s.TokensUsed > maxTokens {
			maxTokens = s.TokensUsed
		}
	}

	gapSpecificity := 0
	if verdict != nil {
		gapSpecificity = verdict.GapSpecificity
	}

	keywords := memory.ExtractKeywords(task)
	topics := memory.ExtractTopics(task)

	return &OrchestratorOutcome{
		OrchestrationID:        octx.OrchestrationID,
		UserID:                 octx.UserID,
		Task:                   task,
		Iterations:             iterations,
		FinalPass:              finalPass,
		TotalHITLDenials:       totalDenials,
		TotalHITLApprovals:     totalApprovals,
		PlanStepCount:          len(summaries),
		ActualStepsNeeded:      len(summaries),
		MaxWorkerSummaryTokens: maxTokens,
		JudgeGapSpecificity:    gapSpecificity,
		EvolutionCount:         evolCount,
		Keywords:               keywords,
		Topics:                 topics,
		PipelineID:             o.pipelineVersion(),
		TotalTokensUsed:        totalTokens,
		TotalToolCalls:         totalTools,
		Duration:               time.Since(octx.StartedAt),
	}
}

// sumTokens sums token usage across all worker summaries.
func sumTokens(summaries []WorkerSummary) int {
	total := 0
	for _, s := range summaries {
		total += s.TokensUsed
	}
	return total
}

// pipelineVersion returns the current pipeline config version.
func (o *Orchestrator) pipelineVersion() string {
	if o.pipeline != nil {
		return o.pipeline.ConfigVersion()
	}
	return "0"
}
