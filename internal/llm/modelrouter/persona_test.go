package modelrouter

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func defaultPersonas() []PersonaOption {
	return []PersonaOption{
		{Name: "reviewer", Description: "reviews code"},
		{Name: "architect", Description: "designs systems"},
	}
}

func personaCfg(srv *systemonetest.Server) testConfig {
	cfg := testCfg(srv)
	cfg.Enabled = false
	return cfg
}

func routePersona(t *testing.T, cfg testConfig, in PersonaInput) PersonaDecision {
	t.Helper()
	e, err := personaTestEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e.RoutePersona(context.Background(), in)
}

func TestRoutePersonaDecisionRules(t *testing.T) {
	tests := []struct {
		name      string
		choice    string
		probs     map[string]float64
		wantMatch bool
		wantReas  string
		want      string
	}{
		{"matched", "architect", map[string]float64{"architect": 0.9, "reviewer": 0.05, "none": 0.05}, true, ReasonMatched, "architect"},
		{"exactly threshold", "reviewer", map[string]float64{"reviewer": 0.6, "none": 0.4}, true, ReasonMatched, "reviewer"},
		{"none", "none", map[string]float64{"none": 0.9, "architect": 0.1}, false, ReasonNoMatch, ""},
		{"low probability", "architect", map[string]float64{"architect": 0.5, "reviewer": 0.3, "none": 0.2}, false, ReasonLowProbability, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := systemonetest.NewOllama035(t)
			srv.SetDecision(tc.choice, tc.probs)
			d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x", Personas: defaultPersonas()})
			if d.Matched != tc.wantMatch || d.Reason != tc.wantReas || d.Persona != tc.want {
				t.Fatalf("got %+v", d)
			}
			if d.Probability != tc.probs[tc.choice] || d.Offered != 2 || d.RouterModel != "tev1:0.8b" {
				t.Fatalf("metadata wrong: %+v", d)
			}
		})
	}
}

func TestRoutePersonaUnknownChoiceIsMalformed(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("ghost", map[string]float64{"ghost": 1})
	d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x", Personas: defaultPersonas()})
	if d.Reason != ReasonRouterError || d.ErrClass != ErrClassMalformed || d.Matched {
		t.Fatalf("got %+v", d)
	}
}

func TestRoutePersonaNoPersonasNoCall(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x"})
	if d.Reason != ReasonNoPersonas || d.Matched || len(srv.Requests()) != 0 {
		t.Fatalf("got %+v reqs=%d", d, len(srv.Requests()))
	}
}

func TestRoutePersonaNoRouterNoCall(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	cfg := personaCfg(srv)
	cfg.Router.Model = ""
	d := routePersona(t, cfg, PersonaInput{Prompt: "x", Personas: defaultPersonas()})
	if d.Reason != ReasonNoRouter || len(srv.Requests()) != 0 {
		t.Fatalf("got %+v reqs=%d", d, len(srv.Requests()))
	}
}

func TestRoutePersonaSinglePersona(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("solo", map[string]float64{"solo": 0.8, "none": 0.2})
	d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x", Personas: []PersonaOption{{Name: "Solo", Description: "d"}}})
	if !d.Matched || d.Persona != "Solo" {
		t.Fatalf("got %+v", d)
	}
}

func TestRoutePersonaErrorClasses(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*systemonetest.Server, *testConfig)
		want  string
	}{
		{"unauthorized", func(s *systemonetest.Server, _ *testConfig) { s.FailNext(401) }, ErrClassUnauthorized},
		{"malformed", func(s *systemonetest.Server, _ *testConfig) { s.SetRawResponse("{nope") }, ErrClassMalformed},
		{"timeout", func(s *systemonetest.Server, c *testConfig) {
			s.Delay(2 * time.Second)
			c.TimeoutMs = 150
		}, ErrClassTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := systemonetest.NewOllama035(t)
			cfg := personaCfg(srv)
			tc.setup(srv, &cfg)
			e, err := newTestEngine(cfg, WithProvider(mustProvider(t, cfg)))
			if err != nil {
				t.Fatal(err)
			}
			e.budgetSet, e.budget, e.budgetAt = true, 2050, time.Now()
			start := time.Now()
			d := e.RoutePersona(context.Background(), PersonaInput{Prompt: "x", Personas: defaultPersonas()})
			if d.Reason != ReasonRouterError || d.Err == nil || d.ErrClass != tc.want || d.Matched {
				t.Fatalf("got %+v", d)
			}
			if tc.want == ErrClassTimeout && time.Since(start) > time.Second {
				t.Fatalf("timeout not honoured: %v", time.Since(start))
			}
		})
	}
}

func TestPersonaKeyMapping(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("code_reviewer", map[string]float64{"code_reviewer": 0.9, "none": 0.1})
	personas := []PersonaOption{
		{Name: "Code Reviewer", Description: "reviews"},
		{Name: "none", Description: "literally none"},
		{Name: "code-reviewer", Description: "collides"},
		{Name: "日本語", Description: "no ascii"},
	}
	d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x", Personas: personas})
	if !d.Matched || d.Persona != "Code Reviewer" {
		t.Fatalf("got %+v", d)
	}
	if _, ok := d.Probabilities["Code Reviewer"]; !ok {
		t.Fatalf("probabilities must be keyed by name: %v", d.Probabilities)
	}
	// Sorted by name: "Code Reviewer", "code-reviewer", "none", "日本語".
	keys := orderedKeys(t, lastSystemOne(t, srv).Questions["persona"].Criteria)
	want := []string{"code_reviewer", "code_reviewer_2", "none_2", "persona", "none"}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
}

func TestRoutePersonaLiteralNoneNameMaps(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("none_2", map[string]float64{"none_2": 0.9, "none": 0.1})
	d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x", Personas: []PersonaOption{{Name: "none"}}})
	if !d.Matched || d.Persona != "none" {
		t.Fatalf("got %+v", d)
	}
	if d.Probabilities["none"] != 0.1 || d.Probabilities["none_2"] != 0.9 {
		t.Fatalf("reserved none entry overwritten: %v", d.Probabilities)
	}
}

func TestWarnDroppedPersonasOnce(t *testing.T) {
	warnedDropped = sync.Map{}
	set := []string{"warn-a", "warn-b"}
	warnDroppedPersonas(set)
	if _, ok := warnedDropped.Load("warn-a\x00warn-b"); !ok {
		t.Fatal("dropped set not recorded")
	}
	n := 0
	warnedDropped.Range(func(_, _ any) bool { n++; return true })
	warnDroppedPersonas(set)
	m := 0
	warnedDropped.Range(func(_, _ any) bool { m++; return true })
	if n != 1 || m != 1 {
		t.Fatalf("want a single recorded set, got %d then %d", n, m)
	}
}

func TestRoutePersonaCap(t *testing.T) {
	var personas []PersonaOption
	for i := 29; i >= 0; i-- { // unsorted on purpose
		personas = append(personas, PersonaOption{Name: fmt.Sprintf("p%02d", i), Description: "d"})
	}
	run := func() PersonaDecision {
		srv := systemonetest.NewOllama035(t)
		srv.SetDecision("none", map[string]float64{"none": 1})
		d := routePersona(t, personaCfg(srv), PersonaInput{Prompt: "x", Personas: personas})
		if n := len(orderedKeys(t, lastSystemOne(t, srv).Questions["persona"].Criteria)); n != 26 {
			t.Fatalf("want 26 criteria, got %d", n)
		}
		return d
	}
	a, b := run(), run()
	if a.Offered != 25 || len(a.Dropped) != 5 || a.Dropped[0] != "p25" || a.Dropped[4] != "p29" {
		t.Fatalf("got offered=%d dropped=%v", a.Offered, a.Dropped)
	}
	if fmt.Sprint(a.Dropped) != fmt.Sprint(b.Dropped) {
		t.Fatal("not deterministic")
	}
}

func TestRoutePersonaDisabledZeroRoutesCustomProvider(t *testing.T) {
	srv := systemonetest.NewRemote(t)
	srv.SetDecision("reviewer", map[string]float64{"reviewer": 0.9, "none": 0.1})
	cfg := personaCfg(srv)
	cfg.Router.Provider = config.DecisionProviderCustom
	d := routePersona(t, cfg, PersonaInput{Prompt: "x", Personas: defaultPersonas()})
	if !d.Matched || d.Persona != "reviewer" {
		t.Fatalf("got %+v", d)
	}
	if ka := lastSystemOne(t, srv).KeepAlive; ka != "" {
		t.Fatalf("keep_alive must not be sent to custom providers, got %q", ka)
	}
}

func combined(t *testing.T, cfg testConfig, p []PersonaOption) (Decision, PersonaDecision) {
	t.Helper()
	e, err := newTestEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e.RouteWithPersona(context.Background(), cfg.ModelAutoModeConfig, Input{Prompt: "refactor this", CoderModel: coder}, PersonaInput{Personas: p})
}

func TestRouteWithPersonaOneRequest(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetQuestionDecision("task", "implementation", map[string]float64{"implementation": 0.9, "none": 0.1})
	srv.SetQuestionDecision("persona", "architect", map[string]float64{"architect": 0.8, "none": 0.2})
	d, pd := combined(t, testCfg(srv, defaultRoutes()...), defaultPersonas())
	if n := srv.Count("/v1/systemone"); n != 1 {
		t.Fatalf("want exactly 1 request, got %d", n)
	}
	if !d.Matched || d.RouteID != "implementation" || d.Candidates[0] != "m-impl" {
		t.Fatalf("decision %+v", d)
	}
	if !pd.Matched || pd.Persona != "architect" {
		t.Fatalf("persona %+v", pd)
	}
	if d.InputTokens == 0 || pd.InputTokens != 0 || pd.CostUSD != nil {
		t.Fatalf("cost must be on the model decision only: d=%d/%v pd=%d/%v", d.InputTokens, d.CostUSD, pd.InputTokens, pd.CostUSD)
	}
	if len(lastSystemOne(t, srv).Questions) != 2 {
		t.Fatal("want two questions")
	}
}

func TestRouteWithPersonaIndependentRules(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetQuestionDecision("task", "implementation", map[string]float64{"implementation": 0.9, "none": 0.1})
	srv.SetQuestionDecision("persona", "architect", map[string]float64{"architect": 0.4, "none": 0.6})
	d, pd := combined(t, testCfg(srv, defaultRoutes()...), defaultPersonas())
	if !d.Matched || pd.Matched || pd.Reason != ReasonLowProbability {
		t.Fatalf("d=%+v pd=%+v", d, pd)
	}
}

func TestRouteWithPersonaNoRoutes(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("architect", map[string]float64{"architect": 0.9, "none": 0.1})
	d, pd := combined(t, personaCfg(srv), defaultPersonas())
	if d.Reason != ReasonNoRoutes || len(d.Candidates) != 1 || d.Candidates[0] != coder {
		t.Fatalf("d=%+v", d)
	}
	if !pd.Matched || pd.Persona != "architect" {
		t.Fatalf("pd=%+v", pd)
	}
	if srv.Count("/v1/systemone") != 1 || len(lastSystemOne(t, srv).Questions) != 1 {
		t.Fatal("want one request with only the persona question")
	}
}

func TestRouteWithPersonaNoPersonas(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("implementation", map[string]float64{"implementation": 0.9, "none": 0.1})
	d, pd := combined(t, testCfg(srv, defaultRoutes()...), nil)
	if !d.Matched || pd.Reason != ReasonNoPersonas {
		t.Fatalf("d=%+v pd=%+v", d, pd)
	}
	sr := lastSystemOne(t, srv)
	if srv.Count("/v1/systemone") != 1 || len(sr.Questions) != 1 {
		t.Fatal("want one request with only the task question")
	}
	if _, ok := sr.Questions["task"]; !ok {
		t.Fatal("task question missing")
	}
}

func TestRouteWithPersonaMissingAnswer(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetRawResponse(`{"model":"tev1:0.8b","answers":{"task":{"type":"choice","choice":"planning","probabilities":{"planning":0.9,"none":0.1},"confidence":0.8}},"usage":{"input_tokens":5,"output_tokens":1}}`)
	d, pd := combined(t, testCfg(srv, defaultRoutes()...), defaultPersonas())
	if !d.Matched || d.RouteID != "planning" {
		t.Fatalf("task must survive: %+v", d)
	}
	if pd.Reason != ReasonRouterError || pd.ErrClass != ErrClassMalformed {
		t.Fatalf("persona must be malformed: %+v", pd)
	}
}

func TestRouteWithPersonaTransportFailure(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.FailNext(500)
	e, err := newTestEngine(testCfg(srv, defaultRoutes()...))
	if err != nil {
		t.Fatal(err)
	}
	e.budgetSet, e.budget, e.budgetAt = true, 2050, time.Now() // keep the failure for systemone
	d, pd := e.RouteWithPersona(context.Background(), config.ModelAutoModeConfig{Enabled: true, Threshold: 0.6, Routes: defaultRoutes()}, Input{Prompt: "x", CoderModel: coder}, PersonaInput{Personas: defaultPersonas()})
	if d.Reason != ReasonRouterError || pd.Reason != ReasonRouterError || d.ErrClass != ErrClassServer || pd.ErrClass != ErrClassServer {
		t.Fatalf("d=%+v pd=%+v", d, pd)
	}
	if len(d.Candidates) != 1 || d.Candidates[0] != coder {
		t.Fatalf("candidates %v", d.Candidates)
	}
}
