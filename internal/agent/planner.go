package agent

import "encoding/json"

// Plan describes the orchestration plan produced by the Planner.
type Plan struct {
	Mode            string     `json:"mode"` // "single" or "orchestrate"
	Steps           []PlanStep `json:"steps"`
	SuccessCriteria []string   `json:"success_criteria"`
	Reasoning       string     `json:"reasoning"`
}

// PlanStep is a single unit of work within a Plan.
type PlanStep struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
	WorkerHint   string   `json:"worker_hint"`
}

// ParsePlan extracts a Plan JSON from text using bracket-matching to find
// the outermost JSON object.
func ParsePlan(text string) (*Plan, error) {
	start := -1
	end := -1
	depth := 0
	for i, c := range text {
		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}
	if start == -1 || end == -1 {
		return nil, &json.SyntaxError{}
	}

	var plan Plan
	if err := json.Unmarshal([]byte(text[start:end]), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}
