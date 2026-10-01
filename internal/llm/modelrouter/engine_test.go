package modelrouter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

const coder models.ModelID = "coder-model"

// testConfig bundles the two config blocks an engine needs; the embedded
// fields are promoted so tests can tweak cfg.Router / cfg.TimeoutMs directly.
type testConfig struct {
	config.ModelAutoModeConfig `json:"auto"`
	config.DecisionModelConfig `json:"decision"`
}

func newTestEngine(cfg testConfig, opts ...EngineOption) (*Engine, error) {
	return NewEngine(cfg.DecisionModelConfig, opts...)
}

func forTestConfig(cfg testConfig) (*Engine, error) {
	return ForConfig(cfg.DecisionModelConfig)
}

func personaTestEngine(cfg testConfig) (*Engine, error) {
	return ForConfig(cfg.DecisionModelConfig)
}

func testCfg(srv *systemonetest.Server, routes ...config.ModelAutoRoute) testConfig {
	return testConfig{
		ModelAutoModeConfig: config.ModelAutoModeConfig{Enabled: true, Threshold: 0.6, Routes: routes},
		DecisionModelConfig: config.DecisionModelConfig{
			Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"},
		},
	}
}

func defaultRoutes() []config.ModelAutoRoute {
	return []config.ModelAutoRoute{
		{ID: "implementation", Description: "write or change code", Model: "m-impl", Fallbacks: []models.ModelID{"m-fb1", "m-impl", "m-fb2"}},
		{ID: "planning", Description: "plan or design", Model: "m-plan"},
		{ID: "quick_question", Description: "short factual question", Model: "m-quick"},
	}
}

func route(t *testing.T, cfg testConfig, in Input) Decision {
	t.Helper()
	e, err := newTestEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if in.CoderModel == "" {
		in.CoderModel = coder
	}
	return e.Route(context.Background(), cfg.ModelAutoModeConfig, in)
}

func TestEngineDecisionRules(t *testing.T) {
	tests := []struct {
		name      string
		choice    string
		probs     map[string]float64
		minConf   float64
		wantMatch bool
		wantReas  string
		wantFirst models.ModelID
	}{
		{"match above threshold", "implementation", map[string]float64{"implementation": 0.94, "planning": 0.03, "none": 0.03}, 0, true, ReasonMatched, "m-impl"},
		{"below threshold", "quick_question", map[string]float64{"quick_question": 0.55, "implementation": 0.25, "none": 0.2}, 0, false, ReasonLowProbability, coder},
		{"exactly threshold", "planning", map[string]float64{"planning": 0.6, "none": 0.4}, 0, true, ReasonMatched, "m-plan"},
		{"none wins", "none", map[string]float64{"none": 0.89, "implementation": 0.11}, 0, false, ReasonNoMatch, coder},
		{"low confidence", "implementation", map[string]float64{"implementation": 0.7, "planning": 0.3}, 0.99, false, ReasonLowConfidence, coder},
		{"confidence guard off", "implementation", map[string]float64{"implementation": 0.7, "planning": 0.3}, 0, true, ReasonMatched, "m-impl"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := systemonetest.NewOllama035(t)
			srv.SetDecision(tc.choice, tc.probs)
			cfg := testCfg(srv, defaultRoutes()...)
			cfg.MinConfidence = tc.minConf
			d := route(t, cfg, Input{Prompt: "do it"})
			if d.Matched != tc.wantMatch || d.Reason != tc.wantReas {
				t.Fatalf("got matched=%v reason=%s, want %v %s", d.Matched, d.Reason, tc.wantMatch, tc.wantReas)
			}
			if d.Candidates[0] != tc.wantFirst {
				t.Fatalf("candidates %v, want first %s", d.Candidates, tc.wantFirst)
			}
			if !tc.wantMatch && (len(d.Candidates) != 1 || d.RouteID != "") {
				t.Fatalf("no-match must yield [coder], got %v route=%q", d.Candidates, d.RouteID)
			}
			if tc.wantMatch {
				if d.RouteID != tc.choice {
					t.Fatalf("route %q", d.RouteID)
				}
				if tc.choice == "implementation" && len(d.Candidates) != 3 {
					t.Fatalf("want [impl fb1 fb2] deduped, got %v", d.Candidates)
				}
			}
			if d.Probability != tc.probs[tc.choice] || d.RouterModel != "tev1:0.8b" || d.RouterProvider != "ollama" {
				t.Fatalf("decision metadata wrong: %+v", d)
			}
		})
	}
}

type sentReq struct {
	Model     string          `json:"model"`
	KeepAlive string          `json:"keep_alive"`
	State     json.RawMessage `json:"state"`
	Questions map[string]struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
		Criteria     json.RawMessage
	} `json:"questions"`
}

func lastSystemOne(t *testing.T, srv *systemonetest.Server) sentReq {
	t.Helper()
	var last []byte
	for _, r := range srv.Requests() {
		if r.Path == "/v1/systemone" {
			last = r.Body
		}
	}
	if last == nil {
		t.Fatal("no systemone request")
	}
	var sr sentReq
	if err := json.Unmarshal(last, &sr); err != nil {
		t.Fatal(err)
	}
	return sr
}

func orderedKeys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for dec.More() {
		tok, _ := dec.Token()
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func TestEngineCriteriaOrder(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	srv.SetDecision("b", map[string]float64{"b": 0.9, "none": 0.1})
	cfg := testCfg(srv,
		config.ModelAutoRoute{ID: "b", Description: "bee", Model: "m-b"},
		config.ModelAutoRoute{ID: "a", Description: "ay", Model: "m-a", Disabled: true},
	)
	d := route(t, cfg, Input{Prompt: "hello"})
	if !d.Matched {
		t.Fatalf("expected match: %+v", d)
	}
	sr := lastSystemOne(t, srv)
	if len(sr.Questions) != 1 {
		t.Fatalf("want exactly one question, got %d", len(sr.Questions))
	}
	q, ok := sr.Questions["task"]
	if !ok || q.Type != "choice" {
		t.Fatalf("question task/choice missing: %+v", sr.Questions)
	}
	if strings.TrimSpace(q.Instructions) == "" {
		t.Fatal("instructions must be non-empty")
	}
	keys := orderedKeys(t, q.Criteria)
	if len(keys) != 2 || keys[0] != "b" || keys[1] != "none" {
		t.Fatalf("criteria keys %v, want [b none]", keys)
	}
	if sr.KeepAlive == "" {
		t.Fatal("keep_alive must be sent for ollama")
	}
	var st map[string]any
	if err := json.Unmarshal(sr.State, &st); err != nil || st["request"] != "hello" {
		t.Fatalf("state should be an object with request: %s", sr.State)
	}
}

func TestEngineKeepAliveOllamaOnly(t *testing.T) {
	srv := systemonetest.NewRemote(t)
	srv.SetDecision("a", map[string]float64{"a": 0.9, "none": 0.1})
	cfg := testCfg(srv, config.ModelAutoRoute{ID: "a", Description: "x", Model: "m"})
	cfg.Router.Provider = config.DecisionProviderCustom
	d := route(t, cfg, Input{Prompt: "p"})
	if !d.Matched {
		t.Fatalf("%+v", d)
	}
	if ka := lastSystemOne(t, srv).KeepAlive; ka != "" {
		t.Fatalf("keep_alive must not be sent to custom providers, got %q", ka)
	}
}

func TestEngineNoRoutesNoCall(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	cfg := testCfg(srv, config.ModelAutoRoute{ID: "a", Description: "x", Model: "m", Disabled: true})
	d := route(t, cfg, Input{Prompt: "hi"})
	if d.Reason != ReasonNoRoutes || d.Matched || len(d.Candidates) != 1 || d.Candidates[0] != coder {
		t.Fatalf("unexpected: %+v", d)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("expected no HTTP requests, got %d", n)
	}
}

func TestEngineRouterErrors(t *testing.T) {
	routes := defaultRoutes()
	cases := []struct {
		name  string
		setup func(*systemonetest.Server, *testConfig)
		want  string
	}{
		{"unauthorized", func(s *systemonetest.Server, _ *testConfig) { s.FailNext(401) }, ErrClassUnauthorized},
		{"model not found", func(s *systemonetest.Server, _ *testConfig) { s.FailNext(404) }, ErrClassModelNotFound},
		{"server", func(s *systemonetest.Server, _ *testConfig) { s.FailNext(500) }, ErrClassServer},
		{"too large", func(s *systemonetest.Server, _ *testConfig) { s.FailNext(413) }, ErrClassTooLarge},
		{"bad request", func(s *systemonetest.Server, _ *testConfig) { s.FailNext(400) }, ErrClassBadRequest},
		{"malformed", func(s *systemonetest.Server, _ *testConfig) { s.SetRawResponse("{nope") }, ErrClassMalformed},
		{"timeout", func(s *systemonetest.Server, c *testConfig) {
			s.Delay(2 * time.Second)
			c.TimeoutMs = 150
		}, ErrClassTimeout},
		{"unreachable", func(s *systemonetest.Server, c *testConfig) {
			c.Router.BaseURL = "http://127.0.0.1:1"
		}, ErrClassUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := systemonetest.NewOllama035(t)
			cfg := testCfg(srv, routes...)
			// Warm the budget lookups out of the failure queue: only the
			// systemone call should consume the scripted failure.
			tc.setup(srv, &cfg)
			e, err := newTestEngine(cfg, WithProvider(mustProvider(t, cfg)))
			if err != nil {
				t.Fatal(err)
			}
			e.budgetSet, e.budget, e.budgetAt = true, 2050, time.Now()
			start := time.Now()
			d := e.Route(context.Background(), cfg.ModelAutoModeConfig, Input{Prompt: "x", CoderModel: coder})
			if d.Reason != ReasonRouterError || d.Err == nil || d.ErrClass != tc.want {
				t.Fatalf("got reason=%s class=%s err=%v, want class %s", d.Reason, d.ErrClass, d.Err, tc.want)
			}
			if len(d.Candidates) != 1 || d.Candidates[0] != coder || d.Matched {
				t.Fatalf("candidates %v", d.Candidates)
			}
			if tc.want == ErrClassTimeout && time.Since(start) > time.Second {
				t.Fatalf("timeout not honoured: %v", time.Since(start))
			}
		})
	}
}

func mustProvider(t *testing.T, cfg testConfig) systemone.DecisionProvider {
	t.Helper()
	e, err := newTestEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e.Provider()
}

func TestForConfigCaching(t *testing.T) {
	srv := systemonetest.NewOllama035(t)
	cfg := testCfg(srv, defaultRoutes()...)
	a, err := forTestConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := forTestConfig(cfg)
	if a != b {
		t.Fatal("same config must reuse the engine")
	}
	cfg.Threshold = 0.8 // consumer policy is not part of the engine key
	if c, _ := forTestConfig(cfg); c != a {
		t.Fatal("a policy change must not rebuild the engine")
	}
	cfg.Router.Model = "other-model"
	c, _ := forTestConfig(cfg)
	if c == a {
		t.Fatal("changed decision model must rebuild the engine")
	}
}
