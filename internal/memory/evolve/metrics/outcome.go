package metrics

import (
	"encoding/json"
	"math"
	"time"
)

type TaskOutcome struct {
	SessionID     string        `json:"session_id"`
	UserID        string        `json:"user_id"`
	Timestamp     time.Time     `json:"timestamp"`
	HITLDenials   int           `json:"hitl_denials"`
	HITLApprovals int           `json:"hitl_approvals"`
	SteerCount    int           `json:"steer_count"`
	FinalStatus   string        `json:"final_status"`
	TokensUsed    int           `json:"tokens_used"`
	ToolCalls     int           `json:"tool_calls"`
	Duration      time.Duration `json:"duration"`
	TaskKeywords  []string      `json:"task_keywords"`
	TaskTopics    []string      `json:"task_topics"`
	SkillsUsed    []string      `json:"skills_used"`
	PipelineID    string        `json:"pipeline_id"`
}

func (o *TaskOutcome) Score() float64 {
	score := 1.0
	score -= float64(o.HITLDenials) * 0.25
	score -= float64(o.SteerCount) * 0.20
	if o.FinalStatus == "aborted" {
		score -= 0.3
	}
	if o.FinalStatus == "error" {
		score -= 0.2
	}
	if o.TokensUsed > 50000 {
		score -= 0.1
	}
	if o.ToolCalls > 30 {
		score -= 0.1
	}
	if score < 0.0 {
		return 0.0
	}
	return math.Round(score*100) / 100
}

func (o *TaskOutcome) MarshalJSON() ([]byte, error) {
	type Alias TaskOutcome
	return json.Marshal(&struct {
		*Alias
		Duration string `json:"duration"`
	}{
		Alias:    (*Alias)(o),
		Duration: o.Duration.String(),
	})
}

func (o *TaskOutcome) UnmarshalJSON(data []byte) error {
	type Alias TaskOutcome
	aux := &struct {
		*Alias
		Duration string `json:"duration"`
	}{
		Alias: (*Alias)(o),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.Duration != "" {
		d, err := time.ParseDuration(aux.Duration)
		if err != nil {
			return err
		}
		o.Duration = d
	}
	return nil
}
