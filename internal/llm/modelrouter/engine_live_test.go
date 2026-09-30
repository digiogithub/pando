package modelrouter

import (
	"context"
	"os"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func TestEngineLiveOllama(t *testing.T) {
	if os.Getenv("PANDO_LIVE_OLLAMA") != "1" {
		t.Skip("set PANDO_LIVE_OLLAMA=1 to run against a local Ollama with tev1:0.8b")
	}
	base := os.Getenv("PANDO_LIVE_OLLAMA_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	cfg := config.ModelAutoModeConfig{
		Enabled:   true,
		Router:    config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: base, Model: "tev1:0.8b"},
		Threshold: 0.6,
		TimeoutMs: 20000,
		Routes: []config.ModelAutoRoute{
			{ID: "implementation", Description: "Write, modify, fix or refactor source code", Model: "m-impl"},
			{ID: "planning", Description: "Plan, design or analyse architecture before coding", Model: "m-plan"},
			{ID: "quick_question", Description: "Short factual question about a tool or concept", Model: "m-quick"},
		},
	}
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"Fix the nil pointer panic in session.go and add a regression test",
		"Design the architecture for a plugin system before we write anything",
		"What does the -race flag do in go test?",
	} {
		d := e.Route(context.Background(), Input{Prompt: p, CoderModel: coder})
		if d.Err != nil {
			t.Fatalf("%q: router error (%s): %v", p, d.ErrClass, d.Err)
		}
		t.Logf("%q -> reason=%s route=%q p=%.2f conf=%.2f %dms", p, d.Reason, d.RouteID, d.Probability, d.Confidence, d.LatencyMs)
	}
}
