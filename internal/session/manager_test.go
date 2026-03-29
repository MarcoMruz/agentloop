package session

import "testing"

// TestShouldAutoApprove_LowRisk verifies that low-risk HITL requests are
// auto-resolved when AutoApproveNonHigh is enabled.
func TestShouldAutoApprove_LowRisk(t *testing.T) {
	if !shouldAutoApprove(true, "low") {
		t.Error("expected low risk to be auto-approved when enabled")
	}
}

// TestShouldAutoApprove_MediumRisk verifies that medium-risk HITL requests are
// auto-resolved when AutoApproveNonHigh is enabled.
func TestShouldAutoApprove_MediumRisk(t *testing.T) {
	if !shouldAutoApprove(true, "medium") {
		t.Error("expected medium risk to be auto-approved when enabled")
	}
}

// TestShouldAutoApprove_HighRisk verifies that high-risk HITL requests always
// block regardless of the AutoApproveNonHigh flag.
func TestShouldAutoApprove_HighRisk(t *testing.T) {
	if shouldAutoApprove(true, "high") {
		t.Error("SECURITY: high-risk request must never be auto-approved")
	}
	if shouldAutoApprove(false, "high") {
		t.Error("SECURITY: high-risk request must never be auto-approved even when disabled")
	}
}

// TestShouldAutoApprove_DisabledFlag verifies that when AutoApproveNonHigh is
// false, no risk level (including low/medium) is auto-approved.
func TestShouldAutoApprove_DisabledFlag(t *testing.T) {
	for _, level := range []string{"low", "medium", "high", ""} {
		if shouldAutoApprove(false, level) {
			t.Errorf("expected no auto-approval when disabled, got true for risk level %q", level)
		}
	}
}

// TestShouldAutoApprove_UnknownRiskLevel verifies that an unknown/empty risk
// level is never auto-approved (defaults to blocking).
func TestShouldAutoApprove_UnknownRiskLevel(t *testing.T) {
	for _, level := range []string{"", "unknown", "critical", "extreme"} {
		if shouldAutoApprove(true, level) {
			t.Errorf("SECURITY: unknown risk level %q must not be auto-approved", level)
		}
	}
}
