package session

import "testing"

func TestComputeConversationContextID_Slack(t *testing.T) {
	req := StartRequest{
		UserID:    "marco",
		Text:      "fix auth",
		Source:    "slack",
		ChannelID: "C123456",
		ThreadID:  "1234567890.000100",
	}
	got := req.ComputeConversationContextID()
	want := "C123456:1234567890.000100"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestComputeConversationContextID_CLI(t *testing.T) {
	req := StartRequest{UserID: "marco", Text: "fix auth", Source: "cli"}
	if got := req.ComputeConversationContextID(); got != "" {
		t.Errorf("expected empty for cli source, got %q", got)
	}
}

func TestComputeConversationContextID_MissingFields(t *testing.T) {
	req := StartRequest{UserID: "marco", Text: "fix auth", Source: "slack"}
	// ThreadID and ChannelID both missing
	if got := req.ComputeConversationContextID(); got != "" {
		t.Errorf("expected empty when fields missing, got %q", got)
	}
}

func TestNewSession_SetsConversationContextID(t *testing.T) {
	sess := NewSession("marco", "fix auth", "/tmp", "slack", "C123456", "1234567890.000100")
	want := "C123456:1234567890.000100"
	if sess.ConversationContextID != want {
		t.Errorf("got %q, want %q", sess.ConversationContextID, want)
	}
	if sess.ChannelID != "C123456" {
		t.Errorf("ChannelID not set")
	}
	if sess.ThreadID != "1234567890.000100" {
		t.Errorf("ThreadID not set")
	}
}

func TestNewSession_EmptyContextID_ForCLI(t *testing.T) {
	sess := NewSession("marco", "fix auth", "/tmp", "cli", "", "")
	if sess.ConversationContextID != "" {
		t.Errorf("expected empty ConversationContextID for cli, got %q", sess.ConversationContextID)
	}
}
