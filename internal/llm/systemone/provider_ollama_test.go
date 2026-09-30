package systemone

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func ids(ms []DecisionModel) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func newOllama(t *testing.T, s *systemonetest.Server) DecisionProvider {
	t.Helper()
	p, err := NewProvider(KindOllama, Options{BaseURL: s.URL})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOllamaListDecisionModels(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	p := newOllama(t, s)
	ms, st, err := p.ListDecisionModels(context.Background(), false)
	if err != nil || st != ListFiltered {
		t.Fatalf("%v %v", err, st)
	}
	if got := ids(ms); len(got) != 1 || got[0] != "tev1:0.8b" {
		t.Fatalf("got %v", got)
	}
	if ms[0].ContextWindow != 262144 || ms[0].Capabilities[0] != "decision" {
		t.Fatalf("%+v", ms[0])
	}

	all, _, _ := p.ListDecisionModels(context.Background(), true)
	if got := strings.Join(ids(all), ","); got != "tev1:0.8b,qwen2.5-coder:0.5b,nomic-embed-text:latest" {
		t.Fatalf("showAll (cloud excluded): %s", got)
	}

	// no decision model pulled
	none := systemonetest.NewOllama035(t, systemonetest.WithTagsJSON(`{"models":[{"name":"nomic-embed-text:latest","capabilities":["embedding"]}]}`))
	ms, st, err = newOllama(t, none).ListDecisionModels(context.Background(), false)
	if err != nil || len(ms) != 0 || st != ListFiltered {
		t.Fatalf("empty: %v %v %v", ms, st, err)
	}
	if !strings.Contains(OllamaPullHint, "ollama pull tev1:0.8b") {
		t.Fatal("hint")
	}

	// old Ollama: no capabilities
	old := systemonetest.NewOllama035(t, systemonetest.WithTagsJSON(`{"models":[{"name":"llama3:8b"}]}`))
	_, st, _ = newOllama(t, old).ListDecisionModels(context.Background(), false)
	if st != ListUnsupported {
		t.Fatalf("want unsupported, got %v", st)
	}
}

func TestOllamaVersionGate(t *testing.T) {
	s := systemonetest.NewOllama035(t, systemonetest.WithVersion("0.32.14"))
	r := newOllama(t, s).Health(context.Background(), "tev1:0.8b")
	if r.OK || r.VersionOK || !r.Reachable {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(strings.Join(r.Problems, "|"), "Upgrade Ollama to ≥ 0.35") {
		t.Fatalf("problems: %v", r.Problems)
	}

	good := systemonetest.NewOllama035(t)
	r = newOllama(t, good).Health(context.Background(), "tev1:0.8b")
	if !r.OK || !r.VersionOK || !r.ModelFound || !r.IsDecision || r.Remote || !r.Authorized || r.Version != "0.35.0" {
		t.Fatalf("%+v", r)
	}
	if good.Requests()[len(good.Requests())-1].Path != "/v1/systemone" {
		t.Fatal("health should probe /v1/systemone")
	}

	r = newOllama(t, good).Health(context.Background(), "qwen2.5-coder:0.5b")
	if r.OK || r.IsDecision || !r.ModelFound {
		t.Fatalf("non-decision: %+v", r)
	}
	r = newOllama(t, good).Health(context.Background(), "nope:1b")
	if r.OK || r.ModelFound {
		t.Fatalf("missing: %+v", r)
	}

	// unreachable
	p, _ := NewProvider(KindOllama, Options{BaseURL: "http://127.0.0.1:1"})
	if r := p.Health(context.Background(), "x"); r.OK || r.Reachable || len(r.Problems) == 0 {
		t.Fatalf("%+v", r)
	}
}

func TestOllamaContextBudget(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	p := newOllama(t, s)
	if n := p.ContextBudget(context.Background(), "tev1:0.8b"); n != 2050 {
		t.Fatalf("num_ctx: %d", n)
	}
	p.ContextBudget(context.Background(), "tev1:0.8b")
	if s.Count("/api/show") != 1 {
		t.Fatal("budget should be cached")
	}

	fb := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(`{"parameters":"","model_info":{"qwen35.context_length":8192}}`))
	if n := newOllama(t, fb).ContextBudget(context.Background(), "m"); n != 8192 {
		t.Fatalf("context_length fallback: %d", n)
	}
	none := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(`{}`))
	if n := newOllama(t, none).ContextBudget(context.Background(), "m"); n != 2048 {
		t.Fatalf("default: %d", n)
	}
	none.FailNext(500)
	if n := newOllama(t, none).ContextBudget(context.Background(), "m2"); n != 2048 {
		t.Fatalf("error: %d", n)
	}
}

func TestOllamaDefaultKeepAlive(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	p := newOllama(t, s)
	if p.Client().KeepAlive() != "30m" {
		t.Fatalf("default keep_alive: %q", p.Client().KeepAlive())
	}
}
