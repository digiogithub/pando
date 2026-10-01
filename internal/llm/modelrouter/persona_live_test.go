package modelrouter

import (
	"context"
	"os"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/mesnada/persona/builtin"
)

func TestPersonaLiveOllama(t *testing.T) {
	if os.Getenv("PANDO_LIVE_OLLAMA") != "1" {
		t.Skip("set PANDO_LIVE_OLLAMA=1 to run against a local Ollama with tev1:0.8b")
	}
	base := os.Getenv("PANDO_LIVE_OLLAMA_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	cfg := testConfig{ModelAutoModeConfig: config.ModelAutoModeConfig{
		Routes: []config.ModelAutoRoute{
			{ID: "implementation", Description: "Write, modify, fix or refactor source code", Model: "m-impl"},
			{ID: "planning", Description: "Plan, design or analyse architecture before coding", Model: "m-plan"},
		},
	}}
	cfg.Router = config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: base, Model: "tev1:0.8b"}
	cfg.TimeoutMs = 20000
	mgr, err := persona.NewManagerWithBuiltins(builtin.FS, "")
	if err != nil {
		t.Fatal(err)
	}
	var personas []PersonaOption
	for name, desc := range mgr.Descriptions() {
		personas = append(personas, PersonaOption{Name: name, Description: desc})
	}
	e, err := newTestEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ prompt, want string }{
		{"Fix the nil pointer panic in session.go and refactor the handler", "software-engineer"},
		{"Write a Dockerfile and a Kubernetes deployment for this service, with a CI pipeline", "system-engineer"},
		{"Write a test plan and regression tests for the login flow, look for edge-case bugs", "qa"},
		{"Explain the difference between TCP and UDP", "assistant"},
		{"yes, do it", ""},
	}
	hits := 0
	for _, c := range cases {
		d := e.RoutePersona(context.Background(), PersonaInput{Prompt: c.prompt, Personas: personas})
		if d.Err != nil {
			t.Fatalf("%q: router error (%s): %v", c.prompt, d.ErrClass, d.Err)
		}
		if d.Persona == c.want {
			hits++
		}
		t.Logf("%q -> reason=%s persona=%q want=%q p=%.2f %dms", c.prompt, d.Reason, d.Persona, c.want, d.Probability, d.LatencyMs)
	}
	t.Logf("accuracy %d/%d", hits, len(cases))

	// One request must yield both decisions.
	dec, pd := e.RouteWithPersona(context.Background(), cfg.ModelAutoModeConfig,
		Input{Prompt: cases[0].prompt, CoderModel: coder},
		PersonaInput{Prompt: cases[0].prompt, Personas: personas})
	if dec.Err != nil || pd.Err != nil {
		t.Fatalf("combined: task err=%v persona err=%v", dec.Err, pd.Err)
	}
	t.Logf("combined -> route=%q (p=%.2f) persona=%q (p=%.2f) %dms", dec.RouteID, dec.Probability, pd.Persona, pd.Probability, dec.LatencyMs)
}
