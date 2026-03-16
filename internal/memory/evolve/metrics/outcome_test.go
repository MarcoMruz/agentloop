package metrics

import (
	"testing"
	"time"
)

func TestScorePerfectTask(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done"}
	if s := o.Score(); s != 1.0 {
		t.Fatalf("expected 1.0, got %f", s)
	}
}

func TestScoreHITLDenials(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", HITLDenials: 2}
	expected := 0.5
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreSteers(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", SteerCount: 3}
	expected := 0.4
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreFloor(t *testing.T) {
	o := TaskOutcome{FinalStatus: "aborted", HITLDenials: 5, SteerCount: 5}
	if s := o.Score(); s != 0.0 {
		t.Fatalf("expected 0.0, got %f", s)
	}
}

func TestScoreAbortedTask(t *testing.T) {
	o := TaskOutcome{FinalStatus: "aborted"}
	expected := 0.7
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreErrorTask(t *testing.T) {
	o := TaskOutcome{FinalStatus: "error"}
	expected := 0.8
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreHighTokenUsage(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", TokensUsed: 60000}
	expected := 0.9
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreHighToolCalls(t *testing.T) {
	o := TaskOutcome{FinalStatus: "done", ToolCalls: 40}
	expected := 0.9
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestScoreCombined(t *testing.T) {
	o := TaskOutcome{
		FinalStatus: "done",
		HITLDenials: 1,
		SteerCount:  1,
		TokensUsed:  60000,
		ToolCalls:   40,
	}
	expected := 0.35
	if s := o.Score(); s != expected {
		t.Fatalf("expected %f, got %f", expected, s)
	}
}

func TestOutcomeJSONRoundTrip(t *testing.T) {
	o := TaskOutcome{
		SessionID:   "sess-abc",
		UserID:      "marco",
		Timestamp:   time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC),
		HITLDenials: 1,
		FinalStatus: "done",
	}
	data, err := o.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var o2 TaskOutcome
	if err := o2.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if o2.SessionID != o.SessionID {
		t.Fatalf("session mismatch: %s != %s", o2.SessionID, o.SessionID)
	}
	if o2.Score() != o.Score() {
		t.Fatalf("score mismatch: %f != %f", o2.Score(), o.Score())
	}
}
