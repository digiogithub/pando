package systemone

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func TestHealthCache(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	p := newOllama(t, s)
	h := NewHealthCache(time.Minute)
	now := time.Now()
	h.now = func() time.Time { return now }

	r1 := h.Get(context.Background(), p, "tev1:0.8b")
	if !r1.OK {
		t.Fatalf("%+v", r1)
	}
	n := len(s.Requests())
	r2 := h.Get(context.Background(), p, "tev1:0.8b")
	if len(s.Requests()) != n || r2.OK != r1.OK {
		t.Fatal("second call must be served from cache")
	}

	// different model and different key are separate entries
	h.Get(context.Background(), p, "qwen2.5-coder:0.5b")
	if len(s.Requests()) == n {
		t.Fatal("model should be part of the key")
	}
	n = len(s.Requests())

	// TTL expiry
	now = now.Add(2 * time.Minute)
	h.Get(context.Background(), p, "tev1:0.8b")
	if len(s.Requests()) == n {
		t.Fatal("expired entry must be re-probed")
	}
	n = len(s.Requests())

	// invalidation on config change
	h.Invalidate()
	h.Get(context.Background(), p, "tev1:0.8b")
	if len(s.Requests()) == n {
		t.Fatal("invalidate must force a probe")
	}
}

func TestWarmupOnReload(t *testing.T) {
	s := systemonetest.NewOllama035(t)
	p, _ := NewProvider(KindOllama, Options{BaseURL: s.URL, KeepAlive: "30m"})
	if err := Warmup(context.Background(), p, "tev1:0.8b"); err != nil {
		t.Fatal(err)
	}
	reqs := s.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/v1/systemone" {
		t.Fatalf("want exactly one warm-up request, got %d", len(reqs))
	}
	var body map[string]any
	_ = json.Unmarshal(reqs[0].Body, &body)
	if body["keep_alive"] != "30m" || body["model"] != "tev1:0.8b" {
		t.Fatalf("body: %s", reqs[0].Body)
	}

	// default keep_alive
	d, _ := NewProvider(KindOllama, Options{BaseURL: s.URL})
	_ = Warmup(context.Background(), d, "tev1:0.8b")
	_ = json.Unmarshal(s.Requests()[1].Body, &body)
	if body["keep_alive"] != "30m" {
		t.Fatal("default keep_alive")
	}

	// remote providers: no-op
	r := systemonetest.NewRemote(t)
	rp, _ := NewProvider(KindTypeSafe, Options{BaseURL: r.URL})
	if err := Warmup(context.Background(), rp, "jev-latest"); err != nil || len(r.Requests()) != 0 {
		t.Fatalf("remote warmup: %v %d", err, len(r.Requests()))
	}

	// failures surface
	s.FailNext(500)
	if err := Warmup(context.Background(), p, "tev1:0.8b"); err == nil {
		t.Fatal("want error")
	}
}
