package agent

import (
	"fmt"
	"time"
)

// WorkerSummary captures the outcome of a single plan step execution.
type WorkerSummary struct {
	StepID        string        `json:"step_id"`
	Status        string        `json:"status"` // "done", "partial", "failed"
	Summary       string        `json:"summary"`
	ToolsUsed     []string      `json:"tools_used"`
	HITLDenials   int           `json:"hitl_denials"`
	HITLApprovals int           `json:"hitl_approvals"`
	TokensUsed    int           `json:"tokens_used"`
	Duration      time.Duration `json:"duration"`
}

// WorkerPool configures concurrency and timeout for plan step execution.
type WorkerPool struct {
	Size    int
	Timeout time.Duration
}

// TopoSort groups PlanSteps into execution layers using Kahn's algorithm.
// Steps within a layer have no inter-dependencies and can run concurrently.
// Steps in layer N+1 depend only on steps in layers 0..N.
// Returns an error if a cyclic dependency is detected.
func TopoSort(steps []PlanStep) ([][]PlanStep, error) {
	// Build in-degree map and adjacency list (step ID → dependent step IDs).
	inDegree := make(map[string]int, len(steps))
	// dependents maps a step ID to the IDs of steps that depend on it.
	dependents := make(map[string][]string, len(steps))
	// index for quick lookup
	byID := make(map[string]PlanStep, len(steps))

	for _, s := range steps {
		byID[s.ID] = s
		if _, ok := inDegree[s.ID]; !ok {
			inDegree[s.ID] = 0
		}
		for _, dep := range s.Dependencies {
			inDegree[s.ID]++
			dependents[dep] = append(dependents[dep], s.ID)
		}
	}

	// Seed the queue with all steps that have no dependencies.
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var layers [][]PlanStep
	processed := 0

	for len(queue) > 0 {
		// Current wave is everything in the queue right now.
		wave := queue
		queue = nil

		var layer []PlanStep
		for _, id := range wave {
			layer = append(layer, byID[id])
			processed++
			for _, depID := range dependents[id] {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					queue = append(queue, depID)
				}
			}
		}
		layers = append(layers, layer)
	}

	if processed != len(steps) {
		return nil, fmt.Errorf("cyclic dependency detected: %d of %d steps unreachable", len(steps)-processed, len(steps))
	}

	return layers, nil
}

// TruncateSummary truncates text so that its estimated token count does not
// exceed maxTokens. Tokens are estimated at 4 characters each (same heuristic
// used by estimateTokens in core.go). If the text already fits, it is returned
// unchanged.
func TruncateSummary(text string, maxTokens int) string {
	maxChars := maxTokens * 4
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars]
}
