package embeddings

import "testing"

func TestModelOf(t *testing.T) {
	e, err := NewOpenAIEmbedder("key", "text-embedding-3-large", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ModelOf(e); got != "text-embedding-3-large" {
		t.Fatalf("ModelOf(openai) = %q", got)
	}
	if got := ModelOf(nil); got != "" {
		t.Fatalf("ModelOf(nil) = %q", got)
	}
}
