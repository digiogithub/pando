package modelrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	"github.com/digiogithub/pando/internal/rag"
)

const bigContext = `{"model_info":{"llama.context_length":32768}}`

func relevanceFor(srv *systemonetest.Server, mutate ...func(*config.DecisionModelConfig)) rag.RelevanceFilter {
	dec := config.DecisionModelConfig{
		Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"},
	}
	for _, m := range mutate {
		m(&dec)
	}
	return NewRelevanceFilter(RelevanceOptions{
		Decision:          func() config.DecisionModelConfig { return dec },
		Threshold:         func() float64 { return 0.6 },
		MaxCandidates:     func() int { return 32 },
		MaxCandidateChars: func() int { return 400 },
		LocalOnly:         func() bool { return true },
	})
}

func mkCands(n int, source string) []rag.RelevanceCandidate {
	out := make([]rag.RelevanceCandidate, n)
	for i := range out {
		out[i] = rag.RelevanceCandidate{Source: source, ID: fmt.Sprintf("%s-%d", source, i), Text: fmt.Sprintf("candidate text %d", i)}
	}
	return out
}

func useful(p float64) map[string]float64 {
	return map[string]float64{"useful": p, "not_useful": 1 - p}
}

func allTrue(keep []bool) bool {
	for _, k := range keep {
		if !k {
			return false
		}
	}
	return true
}

func TestRelevanceDropsBelowThreshold(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
	srv.SetDecision("useful", useful(0.9))
	srv.SetQuestionDecision("c2", "not_useful", useful(0.2))
	srv.SetQuestionDecision("c3", "useful", useful(0.6)) // exactly the threshold: kept
	cands := mkCands(4, rag.SourceKB)
	keep, res := relevanceFor(srv).Filter(context.Background(), "fix the bug", cands)
	want := []bool{true, false, true, true}
	for i := range want {
		if keep[i] != want[i] {
			t.Fatalf("keep = %v, want %v (res %+v)", keep, want, res)
		}
	}
	if !res.Applied || res.Reason != "" || res.Kept != 3 || res.Dropped != 1 || res.BySource[rag.SourceKB] != [2]int{3, 1} {
		t.Fatalf("res = %+v", res)
	}
	if res.Probabilities["kb-1"] != 0.2 || res.Latency <= 0 {
		t.Fatalf("probabilities/latency = %+v", res)
	}
	if n := srv.Count("/v1/systemone"); n != 1 {
		t.Fatalf("requests = %d", n)
	}
	// The request carries the prompt and the candidate text.
	var last struct {
		State     json.RawMessage `json:"state"`
		Questions map[string]struct {
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	reqs := srv.Requests()
	if err := json.Unmarshal(reqs[len(reqs)-1].Body, &last); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(last.State), "fix the bug") || !strings.Contains(last.Questions["c1"].Instructions, "candidate text 0") {
		t.Fatalf("request = %s", reqs[len(reqs)-1].Body)
	}
}

func TestRelevanceFailOpen(t *testing.T) {
	cands := mkCands(3, rag.SourceCode)
	cases := map[string]struct {
		setup  func() *systemonetest.Server
		mutate func(*config.DecisionModelConfig)
		class  string
	}{
		"500": {
			setup: func() *systemonetest.Server {
				s := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
				for i := 0; i < 5; i++ {
					s.FailNext(500)
				}
				return s
			},
			class: ErrClassServer,
		},
		"timeout": {
			setup: func() *systemonetest.Server {
				s := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
				s.Delay(1500 * time.Millisecond)
				return s
			},
			mutate: func(d *config.DecisionModelConfig) { d.TimeoutMs = 100 },
			class:  ErrClassTimeout,
		},
		"401": {
			setup: func() *systemonetest.Server {
				return systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext), systemonetest.WithAPIKey("secret"))
			},
			class: ErrClassUnauthorized,
		},
		"malformed": {
			setup: func() *systemonetest.Server {
				s := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
				s.SetRawResponse("{not json")
				return s
			},
			class: ErrClassMalformed,
		},
		"missing answers": {
			setup: func() *systemonetest.Server {
				s := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
				s.SetRawResponse(`{"model":"m","answers":{},"usage":{}}`)
				return s
			},
			class: ErrClassMalformed,
		},
		"unknown choice": {
			setup: func() *systemonetest.Server {
				s := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
				s.SetDecision("maybe", map[string]float64{"maybe": 1})
				return s
			},
			class: ErrClassMalformed,
		},
		"no router model": {
			setup:  func() *systemonetest.Server { return systemonetest.NewOllama035(t) },
			mutate: func(d *config.DecisionModelConfig) { d.Router.Model = "" },
			class:  RelevanceReasonNoRouter,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := tc.setup()
			var mut []func(*config.DecisionModelConfig)
			if tc.mutate != nil {
				mut = append(mut, tc.mutate)
			}
			keep, res := relevanceFor(srv, mut...).Filter(context.Background(), "prompt", cands)
			if !allTrue(keep) || len(keep) != len(cands) {
				t.Fatalf("fail-open must keep everything: %v", keep)
			}
			if res.Applied || res.Reason != tc.class || res.Dropped != 0 || res.Kept != len(cands) {
				t.Fatalf("res = %+v, want reason %q", res, tc.class)
			}
		})
	}
}

func TestRelevanceBatchesSeventyCandidatesIntoTwoRequests(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
	srv.SetDecision("useful", useful(0.9))
	srv.SetQuestionDecision("c70", "not_useful", useful(0.1)) // lives in the second request
	f := NewRelevanceFilter(RelevanceOptions{
		Decision: func() config.DecisionModelConfig {
			return config.DecisionModelConfig{Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"}}
		},
		Threshold: func() float64 { return 0.6 }, MaxCandidates: func() int { return 100 },
		MaxCandidateChars: func() int { return 400 }, LocalOnly: func() bool { return true },
	})
	keep, res := f.Filter(context.Background(), "p", mkCands(70, rag.SourceKB))
	if n := srv.Count("/v1/systemone"); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	if !res.Applied || res.Dropped != 1 || keep[69] {
		t.Fatalf("res = %+v keep[69]=%v", res, keep[69])
	}
	for _, r := range srv.Requests() {
		if r.Path != "/v1/systemone" {
			continue
		}
		if len(r.Body) > systemone.MaxBodyBytes {
			t.Fatalf("body %d exceeds cap", len(r.Body))
		}
	}
}

func TestRelevanceBeyondCapIsKeptUnseen(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
	srv.SetDecision("not_useful", useful(0.0))
	f := NewRelevanceFilter(RelevanceOptions{
		Decision: func() config.DecisionModelConfig {
			return config.DecisionModelConfig{Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"}}
		},
		Threshold: func() float64 { return 0.6 }, MaxCandidates: func() int { return 3 },
		MaxCandidateChars: func() int { return 400 }, LocalOnly: func() bool { return true },
	})
	// Order is code -> kb -> events -> memory regardless of input order.
	cands := append(mkCands(2, rag.SourceMemory), mkCands(3, rag.SourceCode)...)
	keep, res := f.Filter(context.Background(), "p", cands)
	// 3 code candidates asked and dropped; the 2 memories are beyond the cap and kept.
	for i, c := range cands {
		wantKeep := c.Source == rag.SourceMemory
		if keep[i] != wantKeep {
			t.Fatalf("cand %s keep=%v want %v", c.ID, keep[i], wantKeep)
		}
	}
	if res.Kept != 2 || res.Dropped != 3 {
		t.Fatalf("res = %+v", res)
	}
}

func TestRelevanceCandidateThatCannotFitIsKept(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
	srv.SetDecision("not_useful", useful(0.0))
	f := NewRelevanceFilter(RelevanceOptions{
		Decision: func() config.DecisionModelConfig {
			return config.DecisionModelConfig{Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b"}}
		},
		Threshold: func() float64 { return 0.6 }, MaxCandidates: func() int { return 8 },
		MaxCandidateChars: func() int { return 10_000_000 }, LocalOnly: func() bool { return true },
	})
	cands := []rag.RelevanceCandidate{
		{Source: rag.SourceKB, ID: "huge", Text: strings.Repeat("x", systemone.MaxBodyBytes*2)},
		{Source: rag.SourceKB, ID: "small", Text: "small"},
	}
	keep, res := f.Filter(context.Background(), "p", cands)
	if !keep[0] || keep[1] || !res.Applied {
		t.Fatalf("keep=%v res=%+v", keep, res)
	}
	for _, r := range srv.Requests() {
		if r.Path == "/v1/systemone" && len(r.Body) > systemone.MaxBodyBytes {
			t.Fatalf("oversized body sent: %d", len(r.Body))
		}
	}
}

func TestRelevancePartialBatchFailureKeepsFailedBatch(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
	srv.SetDecision("not_useful", useful(0.0))
	// c70 lands in the second request; an invalid choice makes that batch unusable.
	srv.SetQuestionDecision("c70", "maybe", map[string]float64{"maybe": 1})
	f := relevanceFor(srv)
	f = NewRelevanceFilter(RelevanceOptions{
		Decision: f.(*relevanceFilter).o.Decision, Threshold: func() float64 { return 0.6 },
		MaxCandidates: func() int { return 100 }, MaxCandidateChars: func() int { return 400 },
		LocalOnly: func() bool { return true },
	})
	keep, res := f.Filter(context.Background(), "p", mkCands(70, rag.SourceKB))
	if !res.Applied || res.Reason != RelevanceReasonPartialPfx+ErrClassMalformed {
		t.Fatalf("res = %+v", res)
	}
	dropped, kept := 0, 0
	for i, k := range keep {
		if k {
			kept++
		} else {
			dropped++
		}
		if i >= 64 && !k {
			t.Fatalf("candidate %d of the failed batch must be kept", i)
		}
	}
	if dropped != 64 || kept != 6 {
		t.Fatalf("dropped=%d kept=%d", dropped, kept)
	}
}

func TestRelevanceLocalOnlySkipsHostedProviders(t *testing.T) {
	srv := systemonetest.NewRemote(t)
	dec := config.DecisionModelConfig{Router: config.DecisionRouterConfig{Provider: config.DecisionProviderCustom, BaseURL: srv.URL, Model: "m"}}
	mk := func(local bool) rag.RelevanceFilter {
		return NewRelevanceFilter(RelevanceOptions{
			Decision:  func() config.DecisionModelConfig { return dec },
			LocalOnly: func() bool { return local },
		})
	}
	keep, res := mk(true).Filter(context.Background(), "p", mkCands(2, rag.SourceKB))
	if !allTrue(keep) || res.Applied || res.Reason != RelevanceReasonHosted {
		t.Fatalf("res = %+v", res)
	}
	if n := srv.Count("/v1/systemone"); n != 0 {
		t.Fatalf("hosted provider must not be contacted, got %d requests", n)
	}
	srv.SetDecision("not_useful", useful(0.0))
	keep, res = mk(false).Filter(context.Background(), "p", mkCands(2, rag.SourceKB))
	if !res.Applied || keep[0] || keep[1] {
		t.Fatalf("with LocalOnly off the hosted provider must be used: %+v", res)
	}
}

func TestRelevancePinnedCandidatesAreNotAskedAndKept(t *testing.T) {
	srv := systemonetest.NewOllama035(t, systemonetest.WithShowJSON(bigContext))
	srv.SetDecision("not_useful", useful(0.0))
	cands := mkCands(2, rag.SourceMemory)
	cands[0].Pinned = true
	keep, res := relevanceFor(srv).Filter(context.Background(), "p", cands)
	if !keep[0] || keep[1] || !res.Applied {
		t.Fatalf("keep=%v res=%+v", keep, res)
	}
	reqs := srv.Requests()
	var body struct {
		Questions map[string]json.RawMessage `json:"questions"`
	}
	_ = json.Unmarshal(reqs[len(reqs)-1].Body, &body)
	if len(body.Questions) != 1 {
		t.Fatalf("only the unpinned candidate should be asked, got %d questions", len(body.Questions))
	}
}

// TestRelevanceFailOpenOnOllamaOlderThan035 models an Ollama 0.32 that answers
// /api/version but does not serve /v1/systemone: the filter must keep every
// candidate, report Applied=false with a failure class, and never panic.
func TestRelevanceFailOpenOnOllamaOlderThan035(t *testing.T) {
	var systemoneCalls int
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.32.14"}`))
		case "/v1/systemone":
			systemoneCalls++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(old.Close)

	dec := config.DecisionModelConfig{
		Router: config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, BaseURL: old.URL, Model: "tev1:0.8b"},
	}
	f := NewRelevanceFilter(RelevanceOptions{
		Decision:          func() config.DecisionModelConfig { return dec },
		Threshold:         func() float64 { return 0.6 },
		MaxCandidates:     func() int { return 32 },
		MaxCandidateChars: func() int { return 400 },
		LocalOnly:         func() bool { return true },
	})
	cands := mkCands(3, rag.SourceKB)
	keep, res := f.Filter(context.Background(), "fix the bug", cands)
	if !allTrue(keep) || len(keep) != len(cands) {
		t.Fatalf("unsupported Ollama must keep everything: %v", keep)
	}
	if res.Applied || res.Reason == "" || res.Dropped != 0 || res.Kept != len(cands) {
		t.Fatalf("res = %+v, want Applied=false with an error class", res)
	}
	if res.Probabilities != nil {
		t.Fatalf("no probabilities expected on failure: %+v", res.Probabilities)
	}
}
