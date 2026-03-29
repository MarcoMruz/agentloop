package agent

import "testing"

func TestClassifyRiskLevelHigh(t *testing.T) {
	tests := []struct {
		title string
		rule  string
	}{
		{"Dangerous command blocked", ""},
		{"rm -rf detected", ""},
		{"sudo required", ""},
		{"mkfs operation", ""},
		{"", "dangerous pattern matched"},
	}
	for _, tt := range tests {
		got := classifyRiskLevel(tt.title, tt.rule)
		if got != "high" {
			t.Errorf("classifyRiskLevel(%q, %q) = %q, want \"high\"", tt.title, tt.rule, got)
		}
	}
}

func TestClassifyRiskLevelMedium(t *testing.T) {
	tests := []struct {
		title string
		rule  string
	}{
		{"Docker command requires approval", ""},
		{"File path outside allowed directories", ""},
		{"path restriction violated", ""},
		{"environment variable access", ""},
	}
	for _, tt := range tests {
		got := classifyRiskLevel(tt.title, tt.rule)
		if got != "medium" {
			t.Errorf("classifyRiskLevel(%q, %q) = %q, want \"medium\"", tt.title, tt.rule, got)
		}
	}
}

func TestClassifyRiskLevelLow(t *testing.T) {
	tests := []struct {
		title string
		rule  string
	}{
		{"Permission requested", ""},
		{"write to file", ""},
		{"execute script", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := classifyRiskLevel(tt.title, tt.rule)
		if got != "low" {
			t.Errorf("classifyRiskLevel(%q, %q) = %q, want \"low\"", tt.title, tt.rule, got)
		}
	}
}
