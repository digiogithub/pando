package modelsdev

import (
	"os"
	"testing"
)

// TestLiveCatalogResolvesRealProviderIDs hits the real models.dev endpoint and
// checks that the id spellings the providers actually return still resolve.
// Skipped by default so the suite stays offline; run with
// PANDO_MODELSDEV_LIVE=1 go test ./internal/llm/models/modelsdev -run Live -v
func TestLiveCatalogResolvesRealProviderIDs(t *testing.T) {
	if os.Getenv("PANDO_MODELSDEV_LIVE") == "" {
		t.Skip("set PANDO_MODELSDEV_LIVE=1 to query models.dev")
	}

	catalog, err := Get(t.Context())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	cases := []struct {
		provider string
		modelID  string
	}{
		{"anthropic", "claude-sonnet-4-5-20250929"},
		{"anthropic", "claude-opus-4-8"},
		{"openai", "gpt-5.4"},
		{"github-copilot", "gpt-5.4"},
		{"github-copilot", "claude-sonnet-4.5"},
		{"google", "gemini-2.5-pro"},
		{"openrouter", "anthropic/claude-sonnet-4.5"},
		{"groq", "llama-3.3-70b-versatile"},
	}
	for _, tc := range cases {
		m, ok := catalog.Lookup(tc.provider, tc.modelID)
		if !ok {
			t.Errorf("%s/%s: not found", tc.provider, tc.modelID)
			continue
		}
		t.Logf("%s/%s -> %s in=%v out=%v ctx=%d", tc.provider, tc.modelID, m.ID, m.Cost.Input, m.Cost.Output, m.Limit.Context)
		if m.Limit.Context == 0 {
			t.Errorf("%s/%s: no context limit reported", tc.provider, tc.modelID)
		}
	}
}
