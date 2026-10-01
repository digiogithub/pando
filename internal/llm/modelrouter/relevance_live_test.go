package modelrouter

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/rag"
)

// TestRelevanceLiveOllama runs the real relevance filter against a local
// Ollama >= 0.35 with tev1:0.8b. It is skipped unless PANDO_LIVE_OLLAMA=1,
// like the other live tests in this package.
func TestRelevanceLiveOllama(t *testing.T) {
	if os.Getenv("PANDO_LIVE_OLLAMA") != "1" {
		t.Skip("set PANDO_LIVE_OLLAMA=1 to run against a local Ollama with tev1:0.8b")
	}
	base := os.Getenv("PANDO_LIVE_OLLAMA_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	dec := config.DecisionModelConfig{
		Router:    config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: base, Model: "tev1:0.8b"},
		TimeoutMs: 20000,
	}
	f := NewRelevanceFilter(RelevanceOptions{
		Decision:          func() config.DecisionModelConfig { return dec },
		Threshold:         func() float64 { return 0.6 },
		MaxCandidates:     func() int { return 32 },
		MaxCandidateChars: func() int { return 400 },
		LocalOnly:         func() bool { return true },
	})
	cands := []rag.RelevanceCandidate{
		{Source: rag.SourceCode, ID: "useful", Text: "function RunMigrations — internal/config/decision_model.go:120: copies the legacy modelAutoMode.router block into decisionModel.router when it is empty"},
		{Source: rag.SourceKB, ID: "unrelated", Text: "docs/recipes/lasagna.md: Layer the pasta sheets with bechamel and ragu, then bake for 40 minutes"},
	}
	prompt := "Fix the migration that copies modelAutoMode.router into the decisionModel config block"

	start := time.Now()
	keep, res := f.Filter(context.Background(), prompt, cands)
	elapsed := time.Since(start)
	t.Logf("applied=%v reason=%q kept=%d dropped=%d latency=%s probs=%v", res.Applied, res.Reason, res.Kept, res.Dropped, elapsed, res.Probabilities)

	if !res.Applied || res.Reason != "" {
		t.Fatalf("filter was not applied: %+v", res)
	}
	if limit := dec.EffectiveTimeout() + relevanceGrace + time.Second; elapsed > limit {
		t.Fatalf("filter took %s, over the %s bound", elapsed, limit)
	}
	if !keep[0] {
		t.Errorf("obviously useful candidate was dropped (p=%.2f)", res.Probabilities["useful"])
	}
	if keep[1] {
		t.Errorf("obviously unrelated candidate was kept (p=%.2f)", res.Probabilities["unrelated"])
	}
}
