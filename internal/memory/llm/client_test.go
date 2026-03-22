package llm

import (
	"testing"
)

func TestNoopClientEmbed(t *testing.T) {
	c := &NoopClient{}
	emb, err := c.Embed("anything")
	if err != nil {
		t.Fatal(err)
	}
	if emb != nil {
		t.Fatal("NoopClient.Embed should return nil embedding")
	}
}

func TestNoopClientComplete(t *testing.T) {
	c := &NoopClient{}
	out, err := c.Complete("anything")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("NoopClient.Complete should return empty string, got %q", out)
	}
}
