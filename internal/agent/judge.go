package agent

import "encoding/json"

// JudgeVerdict is the verdict produced by the Judge after evaluating worker output.
type JudgeVerdict struct {
	Pass           bool       `json:"pass"`
	Reasoning      string     `json:"reasoning"`
	Gaps           []JudgeGap `json:"gaps"`
	GapSpecificity int        `json:"gap_specificity"`
	Iteration      int        `json:"iteration"`
}

// JudgeGap describes a specific gap between the worker output and a success criterion.
type JudgeGap struct {
	CriterionID  string `json:"criterion_id"`
	Description  string `json:"description"`
	Evidence     string `json:"evidence"`
	SuggestedFix string `json:"suggested_fix"`
}

// ParseVerdict extracts a JudgeVerdict JSON from text using bracket-matching to find
// the outermost JSON object.
func ParseVerdict(text string) (*JudgeVerdict, error) {
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

	var verdict JudgeVerdict
	if err := json.Unmarshal([]byte(text[start:end]), &verdict); err != nil {
		return nil, err
	}
	return &verdict, nil
}
