package memory

import "testing"

type mockLLM struct{ result string }

func (m *mockLLM) Embed(_ string) ([]float32, error) { return nil, nil }
func (m *mockLLM) Complete(_ string) (string, error) { return m.result, nil }

func TestExtractDeltaWithPrefix(t *testing.T) {
	delta, err := extractDelta(&mockLLM{result: "DELTA: user prefers short responses"}, "conversation text")
	if err != nil {
		t.Fatal(err)
	}
	if delta != "user prefers short responses" {
		t.Fatalf("unexpected delta: %q", delta)
	}
}

func TestExtractDeltaNoPrefix(t *testing.T) {
	delta, err := extractDelta(&mockLLM{result: "nothing actionable here"}, "conversation text")
	if err != nil {
		t.Fatal(err)
	}
	if delta != "" {
		t.Fatalf("expected empty delta when no DELTA: prefix, got %q", delta)
	}
}

func TestExtractDeltaNilClient(t *testing.T) {
	delta, err := extractDelta(nil, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if delta != "" {
		t.Fatal("nil client should return empty delta")
	}
}

func TestExtractDeltaEmptyResponse(t *testing.T) {
	delta, err := extractDelta(&mockLLM{result: ""}, "conversation text")
	if err != nil {
		t.Fatal(err)
	}
	if delta != "" {
		t.Fatal("empty response should yield empty delta")
	}
}
