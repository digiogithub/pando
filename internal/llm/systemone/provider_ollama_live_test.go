package systemone

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestOllamaLive runs against a real local Ollama >= 0.35 with tev1:0.8b.
// Opt-in: PANDO_LIVE_OLLAMA=1 (PANDO_LIVE_OLLAMA_URL overrides the base URL).
func TestOllamaLive(t *testing.T) {
	if os.Getenv("PANDO_LIVE_OLLAMA") != "1" {
		t.Skip("set PANDO_LIVE_OLLAMA=1 to run")
	}
	base := os.Getenv("PANDO_LIVE_OLLAMA_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	const model = "tev1:0.8b"
	p, err := NewProvider(KindOllama, Options{BaseURL: base, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ms, st, err := p.ListDecisionModels(ctx, false)
	if err != nil || st != ListFiltered {
		t.Fatalf("list: %v %v", st, err)
	}
	found := false
	for _, m := range ms {
		if m.ID == model {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s not listed: %v", model, ids(ms))
	}
	if err := Warmup(ctx, p, model); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	r := p.Health(ctx, model)
	if !r.OK {
		t.Fatalf("health: %+v", r)
	}
	t.Logf("health: %+v", r)
	if r.LatencyMs >= 1000 {
		t.Errorf("latency %d ms >= 1000", r.LatencyMs)
	}
	if n := p.ContextBudget(ctx, model); n <= 0 {
		t.Errorf("budget %d", n)
	} else {
		t.Logf("context budget: %d", n)
	}
}
