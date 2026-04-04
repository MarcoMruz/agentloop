package server

import "testing"

func TestExtractFeedbackPrefix(t *testing.T) {
	cases := []struct {
		input    string
		wantText string
		wantOK   bool
	}{
		// colon prefix variations
		{"feedback: the agent was wrong", "the agent was wrong", true},
		{"feedback:the agent was wrong", "the agent was wrong", true},
		{"FEEDBACK: uppercase", "uppercase", true},
		{"Feedback:mixed case", "mixed case", true},
		{"  feedback:  trimmed spaces  ", "trimmed spaces", true},

		// slash-command style
		{"/feedback this was bad", "this was bad", true},
		{"/FEEDBACK uppercase slash", "uppercase slash", true},
		{"  /feedback   leading spaces  ", "leading spaces", true},

		// non-feedback inputs — must not match
		{"fix the login bug", "", false},
		{"task: do something", "", false},
		{"refactor the auth module", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		gotText, gotOK := extractFeedbackPrefix(tc.input)
		if gotOK != tc.wantOK {
			t.Errorf("extractFeedbackPrefix(%q): ok=%v, want %v", tc.input, gotOK, tc.wantOK)
			continue
		}
		if gotOK && gotText != tc.wantText {
			t.Errorf("extractFeedbackPrefix(%q): text=%q, want %q", tc.input, gotText, tc.wantText)
		}
	}
}
