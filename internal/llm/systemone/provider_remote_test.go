package systemone

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
)

func newRemote(t *testing.T, kind ProviderKind, s *systemonetest.Server, key string) DecisionProvider {
	t.Helper()
	p, err := NewProvider(kind, Options{BaseURL: s.URL, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTypeSafeListModels(t *testing.T) {
	s := systemonetest.NewRemote(t, systemonetest.WithAPIKey("good"))
	p := newRemote(t, KindTypeSafe, s, "good")
	ms, st, err := p.ListDecisionModels(context.Background(), false)
	if err != nil || st != ListFiltered {
		t.Fatalf("%v %v", err, st)
	}
	if got := strings.Join(ids(ms), ","); got != "jev-latest,jev-1.13" {
		t.Fatalf("got %s", got)
	}
	if n := p.ContextBudget(context.Background(), "jev-latest"); n != 32768 {
		t.Fatalf("budget %d", n)
	}
	if p.Kind() != KindTypeSafe {
		t.Fatal("kind")
	}
	if h := s.Requests()[0].Header.Get("Authorization"); h != "Bearer good" {
		t.Fatalf("auth: %q", h)
	}
}

func TestRemoteUnauthorized(t *testing.T) {
	s := systemonetest.NewRemote(t, systemonetest.WithAPIKey("good"), systemonetest.WithAcceptedModels("jev-latest", "jev-1.13"))
	p := newRemote(t, KindTypeSafe, s, "bad")
	r := p.Health(context.Background(), "jev-latest")
	if r.OK || r.Authorized || !r.Remote || !r.Reachable {
		t.Fatalf("%+v", r)
	}
	if _, st, err := p.ListDecisionModels(context.Background(), false); !errors.Is(err, ErrUnauthorized) || st != ListUnsupported {
		t.Fatalf("list: %v %v", st, err)
	}

	good := newRemote(t, KindTypeSafe, s, "good")
	r = good.Health(context.Background(), "jev-latest")
	if !r.OK || !r.Authorized || !r.Remote || !r.ModelFound || !r.IsDecision {
		t.Fatalf("%+v", r)
	}
	r = good.Health(context.Background(), "ghost")
	if r.OK || r.ModelFound {
		t.Fatalf("ghost: %+v", r)
	}
}

func TestCustomCatalogueFiltering(t *testing.T) {
	s := systemonetest.NewRemote(t, systemonetest.WithModelsJSON(systemonetest.OpenRouterModelsJSON))
	p := newRemote(t, KindCustom, s, "k")
	ms, st, err := p.ListDecisionModels(context.Background(), false)
	if err != nil || st != ListUnfiltered {
		t.Fatalf("%v %v", err, st)
	}
	if got := strings.Join(ids(ms), ","); got != "typesafe/jev-1.13,~typesafe/jev-latest" {
		t.Fatalf("got %s", got)
	}
	all, _, _ := p.ListDecisionModels(context.Background(), true)
	if len(all) != 3 {
		t.Fatalf("showAll: %v", ids(all))
	}

	// nothing matches: the whole catalogue is returned
	other := systemonetest.NewRemote(t, systemonetest.WithModelsJSON(`{"models":[{"id":"gpt-5"},{"id":"claude"}]}`))
	ms, st, _ = newRemote(t, KindCustom, other, "").ListDecisionModels(context.Background(), false)
	if st != ListUnfiltered || len(ms) != 2 {
		t.Fatalf("%v %v", st, ids(ms))
	}

	// custom budget is configurable
	c, _ := NewProvider(KindCustom, Options{BaseURL: s.URL, ContextBudget: 4096})
	if c.ContextBudget(context.Background(), "x") != 4096 {
		t.Fatal("custom budget")
	}
	if _, err := NewProvider(KindCustom, Options{}); err == nil {
		t.Fatal("custom needs base URL")
	}
	if _, err := NewProvider("nope", Options{}); err == nil {
		t.Fatal("unknown kind")
	}
}

func TestCustomNoModelsEndpoint(t *testing.T) {
	s := systemonetest.NewRemote(t, systemonetest.WithModelsStatus(404), systemonetest.WithAcceptedModels("my/jev"))
	p := newRemote(t, KindCustom, s, "k")
	ms, st, err := p.ListDecisionModels(context.Background(), false)
	if err != nil || st != ListUnsupported || len(ms) != 0 {
		t.Fatalf("%v %v %v", ms, st, err)
	}
	r := p.Health(context.Background(), "my/jev")
	if !r.OK || !r.ModelFound || !r.Remote {
		t.Fatalf("%+v", r)
	}
	r = p.Health(context.Background(), "other")
	if r.OK || r.ModelFound {
		t.Fatalf("%+v", r)
	}
}
