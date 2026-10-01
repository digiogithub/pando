package modelrouter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func relevanceQuestion() systemone.Question {
	return systemone.Question{Type: "choice", Instructions: "Is it relevant?", Criteria: []systemone.Criterion{
		systemone.NewCriterion("yes", "relevant"), systemone.NewCriterion("no", "not relevant"),
	}}
}

func TestEngineAskAnswersManyQuestions(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("yes", map[string]float64{"yes": 0.9, "no": 0.1})
	e, err := ForConfig(testCfg(srv).DecisionModelConfig)
	if err != nil {
		t.Fatal(err)
	}
	qs := map[string]systemone.Question{"a": relevanceQuestion(), "b": relevanceQuestion()}
	resp, lat, err := e.Ask(context.Background(), `{"request":"x"}`, qs)
	if err != nil || resp == nil || lat <= 0 {
		t.Fatalf("resp=%v lat=%v err=%v", resp, lat, err)
	}
	if resp.Answers["a"].Choice != "yes" || resp.Answers["b"].Choice != "yes" {
		t.Fatalf("answers = %+v", resp.Answers)
	}
	if n := srv.Count("/v1/systemone"); n != 1 {
		t.Fatalf("systemone calls = %d", n)
	}
	if e.ContextBudget(context.Background()) <= 0 {
		t.Fatal("context budget must be positive")
	}
}

func TestEngineAskRejections(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	e, err := ForConfig(testCfg(srv).DecisionModelConfig)
	if err != nil {
		t.Fatal(err)
	}
	tooMany := map[string]systemone.Question{}
	for i := 0; i <= systemone.MaxQuestions; i++ {
		tooMany["q"+string(rune('A'+i/26))+string(rune('a'+i%26))] = relevanceQuestion()
	}
	_, _, err = e.Ask(context.Background(), `{}`, tooMany)
	if !errors.Is(err, systemone.ErrBadRequest) || ClassifyError(err) != ErrClassBadRequest {
		t.Fatalf("too many questions: err=%v class=%s", err, ClassifyError(err))
	}
	big := `{"request":"` + strings.Repeat("x", systemone.MaxBodyBytes) + `"}`
	_, _, err = e.Ask(context.Background(), big, map[string]systemone.Question{"a": relevanceQuestion()})
	if !errors.Is(err, systemone.ErrTooLarge) || ClassifyError(err) != ErrClassTooLarge {
		t.Fatalf("big state: err=%v class=%s", err, ClassifyError(err))
	}
	if n := srv.Count("/v1/systemone"); n != 0 {
		t.Fatalf("rejected requests must not be sent, got %d", n)
	}
}

// The routing state is truncated to the model context budget before Ask.
func TestEngineAskStateTruncatedToBudget(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(`{"model_info":{"llama.context_length":512}}`))
	cfg := testCfg(srv, defaultRoutes()...)
	e, err := newTestEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("lorem ipsum dolor ", 2000)
	d := e.Route(context.Background(), cfg.ModelAutoModeConfig, Input{Prompt: huge, CoderModel: coder})
	if d.Err != nil {
		t.Fatalf("route: %v", d.Err)
	}
	last := lastSystemOne(t, srv)
	var st struct{ Request string }
	if err := json.Unmarshal(last.State, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Request) >= len(huge) || !strings.Contains(st.Request, "[…]") {
		t.Fatalf("request not truncated: %d of %d bytes", len(st.Request), len(huge))
	}
	if est := EstimateTokens(string(last.State)); est > 512 {
		t.Fatalf("state estimated at %d tokens, budget 512", est)
	}
}

func TestForConfigHotReloadSwitchesProvider(t *testing.T) {
	a := systemonetest.NewOllama035(t)
	b := systemonetest.NewOllama035(t)
	cfgA, cfgB := testCfg(a, defaultRoutes()...), testCfg(b, defaultRoutes()...)
	for _, cfg := range []testConfig{cfgA, cfgB, cfgA} {
		e, err := forTestConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		e.Route(context.Background(), cfg.ModelAutoModeConfig, Input{Prompt: "x", CoderModel: coder})
	}
	if a.Count("/v1/systemone") != 2 || b.Count("/v1/systemone") != 1 {
		t.Fatalf("calls a=%d b=%d", a.Count("/v1/systemone"), b.Count("/v1/systemone"))
	}
}

func TestPersonaRoutingWithAutoModeDisabled(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("none", map[string]float64{"none": 1})
	dec := config.DecisionModelConfig{Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"}}
	e, err := ForConfig(dec)
	if err != nil {
		t.Fatal(err)
	}
	pd := e.RoutePersona(context.Background(), PersonaInput{Prompt: "x", Personas: defaultPersonas()})
	if pd.Err != nil || pd.Reason == ReasonNoRouter || srv.Count("/v1/systemone") != 1 {
		t.Fatalf("pd=%+v calls=%d", pd, srv.Count("/v1/systemone"))
	}
}

func TestWarmupRunsOnlyWithAConsumerEnabled(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	c := &config.Config{DecisionModel: testCfg(srv).DecisionModelConfig}

	if done := startWarmup(c); done != nil {
		t.Fatal("warm-up must not run when no consumer is on")
	}
	c.Remembrances.ContextEnrichmentDecisionFilterEnabled = true
	done := startWarmup(c)
	if done == nil {
		t.Fatal("warm-up must run with only the context filter enabled")
	}
	<-done
	if srv.Count("/v1/systemone") == 0 {
		t.Fatal("warm-up sent no request")
	}
	c.DecisionModel.Router.Model = ""
	if startWarmup(c) != nil {
		t.Fatal("warm-up must not run without a decision model")
	}
}

func TestWarmupInvalidatesHealth(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	dec := testCfg(srv).DecisionModelConfig
	if _, err := RouterHealth(context.Background(), dec); err != nil {
		t.Fatal(err)
	}
	before := srv.Count("/api/version")
	_, _ = RouterHealth(context.Background(), dec)
	if srv.Count("/api/version") != before {
		t.Fatal("second health call should be cached")
	}
	startWarmup(&config.Config{DecisionModel: dec})
	_, _ = RouterHealth(context.Background(), dec)
	if srv.Count("/api/version") == before {
		t.Fatal("health entry must be dropped on a reload")
	}
}
