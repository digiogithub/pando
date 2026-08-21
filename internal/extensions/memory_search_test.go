package extensions

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/pkg/extension"
)

type wrapExt struct {
	baseExt
	panics bool
	// extra is appended to whatever the inner searcher returned.
	extra []extension.Remembrance
	// fail makes the wrapper return an error instead of calling on.
	fail bool
	// sawQuery records the query the wrapper was given.
	sawQuery string
}

func (e *wrapExt) ExtensionInfo() extension.Info { return e.info(e) }

func (e *wrapExt) WrapRemembranceSearch(next extension.RemembranceSearcher) extension.RemembranceSearcher {
	if e.panics {
		panic("boom")
	}
	return extension.RemembranceSearcherFunc(func(ctx context.Context, q extension.RemembranceQuery) ([]extension.Remembrance, error) {
		e.sawQuery = q.Query
		if e.fail {
			return nil, errors.New("corporate store unreachable")
		}
		local, err := next.SearchRemembrances(ctx, q)
		if err != nil {
			return nil, err
		}
		return append(local, e.extra...), nil
	})
}

func localResults(paths ...string) kb.SearchNext {
	return func(_ context.Context, _ string, _ int, _ kb.SearchOptions) ([]kb.SearchResult, error) {
		out := make([]kb.SearchResult, 0, len(paths))
		for i, p := range paths {
			out = append(out, kb.SearchResult{
				Document:     kb.Document{FilePath: p},
				ChunkContent: "local " + p,
				Score:        float64(len(paths) - i),
			})
		}
		return out, nil
	}
}

func TestSearchMiddlewareNilWithoutWrappers(t *testing.T) {
	if SearchMiddleware(managerWith(t)) != nil {
		t.Fatal("middleware built with no wrapper registered")
	}
	if SearchMiddleware(nil) != nil {
		t.Fatal("middleware built from a nil manager")
	}
}

func TestSearchMiddlewareMergesAndLabels(t *testing.T) {
	w := &wrapExt{
		baseExt: baseExt{id: "memory.store.corp"},
		extra: []extension.Remembrance{
			{Path: "corp/a.md", Content: "remote", Score: 9, Source: "corp"},
		},
	}
	mw := SearchMiddleware(managerWith(t, w))
	if mw == nil {
		t.Fatal("middleware not built")
	}

	got, err := mw(context.Background(), "q", 5, kb.SearchOptions{}, localResults("local/a.md"))
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d, want 2", len(got))
	}
	if w.sawQuery != "q" {
		t.Fatalf("wrapper saw query %q", w.sawQuery)
	}
	// The local hit stays unlabelled, the remote one is marked. An unmarked
	// remote hit is exactly what the visibility rule forbids.
	if _, ok := got[0].Document.Metadata[remoteSourceKey]; ok {
		t.Fatalf("local hit labelled remote: %+v", got[0].Document.Metadata)
	}
	if src, _ := got[1].Document.Metadata[remoteSourceKey].(string); src != "corp" {
		t.Fatalf("remote hit not labelled: %+v", got[1].Document.Metadata)
	}
}

// A corporate store being down must cost the agent the remote hits, never its
// own memory.
func TestSearchMiddlewareFallsBackOnError(t *testing.T) {
	w := &wrapExt{baseExt: baseExt{id: "memory.store.corp"}, fail: true}
	mw := SearchMiddleware(managerWith(t, w))

	got, err := mw(context.Background(), "q", 5, kb.SearchOptions{}, localResults("local/a.md", "local/b.md"))
	if err != nil {
		t.Fatalf("middleware returned an error instead of falling back: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d, want the 2 local hits", len(got))
	}
}

func TestSearchMiddlewareContainsPanic(t *testing.T) {
	bad := &wrapExt{baseExt: baseExt{id: "memory.store.bad"}, panics: true}
	good := &wrapExt{
		baseExt: baseExt{id: "memory.store.good"},
		extra:   []extension.Remembrance{{Path: "corp/a.md", Source: "corp"}},
	}
	mw := SearchMiddleware(managerWith(t, bad, good))

	got, err := mw(context.Background(), "q", 5, kb.SearchOptions{}, localResults("local/a.md"))
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("results = %d; a panicking wrapper cost the working one its results", len(got))
	}
}

// Registration order decides who is outermost, and the order must be stable:
// a merge policy that depends on load order nobody can predict is not a policy.
func TestSearchMiddlewareChainOrder(t *testing.T) {
	first := &wrapExt{baseExt: baseExt{id: "memory.store.a"},
		extra: []extension.Remembrance{{Path: "a", Source: "a"}}}
	second := &wrapExt{baseExt: baseExt{id: "memory.store.b"},
		extra: []extension.Remembrance{{Path: "b", Source: "b"}}}
	mw := SearchMiddleware(managerWith(t, first, second))

	got, err := mw(context.Background(), "q", 5, kb.SearchOptions{}, localResults("local"))
	if err != nil {
		t.Fatalf("middleware: %v", err)
	}
	want := []string{"local", "b", "a"}
	if len(got) != len(want) {
		t.Fatalf("results = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Document.FilePath != w {
			t.Fatalf("result %d = %q, want %q", i, got[i].Document.FilePath, w)
		}
	}
}
