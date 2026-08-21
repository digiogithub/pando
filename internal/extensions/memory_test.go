package extensions

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/rag/kb"
	"github.com/digiogithub/pando/pkg/extension"
)

type sinkExt struct {
	baseExt
	panics bool
	err    error

	mu     sync.Mutex
	events []extension.MemoryEvent
	status extension.MemorySyncStatus
}

func (e *sinkExt) ExtensionInfo() extension.Info { return e.info(e) }

func (e *sinkExt) OnMemoryWrite(_ context.Context, ev extension.MemoryEvent) error {
	if e.panics {
		panic("boom")
	}
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
	return e.err
}

func (e *sinkExt) seen() []extension.MemoryEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]extension.MemoryEvent(nil), e.events...)
}

// reportingSink also implements MemorySyncReporter.
type reportingSink struct{ sinkExt }

func (e *reportingSink) ExtensionInfo() extension.Info                { return e.info(e) }
func (e *reportingSink) MemorySyncStatus() extension.MemorySyncStatus { return e.status }

// syncCfg is the configuration used by most tests: synchronous delivery, so an
// assertion right after the write is not a race.
func syncCfg(scopes ...string) config.ExtensionsMemoryConfig {
	return config.ExtensionsMemoryConfig{Enabled: true, Mode: "sync", Scopes: scopes}
}

func TestPublisherOffByDefault(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)

	if p := NewMemoryPublisher(mgr, config.ExtensionsMemoryConfig{}, nil); p != nil {
		t.Fatal("publisher built with the capability disabled")
	}
}

// The gate needs two keys. Enabling the capability without naming a scope
// shares nothing, and must not silently behave like "everything".
func TestPublisherRequiresScopes(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)

	if p := NewMemoryPublisher(mgr, config.ExtensionsMemoryConfig{Enabled: true}, nil); p != nil {
		t.Fatal("publisher built with an empty scope list")
	}
}

func TestPublisherNoSinks(t *testing.T) {
	mgr := managerWith(t)
	if p := NewMemoryPublisher(mgr, syncCfg("project/"), nil); p != nil {
		t.Fatal("publisher built with no sink registered")
	}
}

func TestPublisherDeliversAllowedScope(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	p := NewMemoryPublisher(mgr, syncCfg("project/"), func() Attribution {
		return Attribution{ProjectID: "/w", InstanceID: "i1"}
	})
	if p == nil {
		t.Fatal("publisher not built")
	}
	t.Cleanup(p.Close)

	p.Observer()(context.Background(), kb.WriteEvent{
		Kind: kb.WriteKindMemory, Op: kb.WriteCreated,
		FilePath: "project/a.md", Key: "k", Scope: "project/",
		Content: "body", Origin: "tool",
	})

	got := sink.seen()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.Kind != extension.KindMemory || ev.Op != extension.MemoryCreated {
		t.Fatalf("kind/op = %v/%v", ev.Kind, ev.Op)
	}
	if ev.Key != "k" || ev.Path != "project/a.md" || ev.Content != "body" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.ProjectID != "/w" || ev.InstanceID != "i1" {
		t.Fatalf("attribution = %+v", ev)
	}
	if ev.Origin != extension.OriginTool {
		t.Fatalf("origin = %q", ev.Origin)
	}
}

func TestPublisherFiltersScope(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	p := NewMemoryPublisher(mgr, syncCfg("project/"), nil)
	t.Cleanup(p.Close)

	p.Observer()(context.Background(), kb.WriteEvent{Scope: "user/", FilePath: "user/a.md"})

	if n := len(sink.seen()); n != 0 {
		t.Fatalf("events = %d, want 0", n)
	}
	if got := p.Stats().Filtered; got != 1 {
		t.Fatalf("filtered = %d, want 1", got)
	}
}

// A scopeless document is not covered by "project/". Sharing it has to be
// asked for explicitly, with "" in the scope list.
func TestPublisherScopelessDocumentNeedsExplicitEmptyScope(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)

	p := NewMemoryPublisher(mgr, syncCfg("project/"), nil)
	t.Cleanup(p.Close)
	p.Observer()(context.Background(), kb.WriteEvent{FilePath: "notes/a.md"})
	if n := len(sink.seen()); n != 0 {
		t.Fatalf("scopeless document published under a scoped gate (%d events)", n)
	}

	sink2 := &sinkExt{baseExt: baseExt{id: "memory.sink.corp2"}}
	mgr2 := managerWith(t, sink2)
	p2 := NewMemoryPublisher(mgr2, syncCfg("project/", ""), nil)
	t.Cleanup(p2.Close)
	p2.Observer()(context.Background(), kb.WriteEvent{FilePath: "notes/a.md"})
	if n := len(sink2.seen()); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

func TestPublisherFiltersPathsAndOrigins(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	cfg := syncCfg("project/")
	cfg.Paths = []string{"project/shared/"}
	cfg.Origins = []string{"tool"}
	p := NewMemoryPublisher(mgr, cfg, nil)
	t.Cleanup(p.Close)

	obs := p.Observer()
	obs(context.Background(), kb.WriteEvent{Scope: "project/", FilePath: "project/private/a.md", Origin: "tool"})
	obs(context.Background(), kb.WriteEvent{Scope: "project/", FilePath: "project/shared/a.md", Origin: "sync"})
	obs(context.Background(), kb.WriteEvent{Scope: "project/", FilePath: "project/shared/b.md", Origin: "tool"})

	got := sink.seen()
	if len(got) != 1 || got[0].Path != "project/shared/b.md" {
		t.Fatalf("events = %+v", got)
	}
}

// An event that came from a remote store is never pushed back out, whatever
// the origin filter says: that is a sync loop, not a policy choice.
func TestPublisherNeverRepublishesRemote(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	cfg := syncCfg("project/")
	cfg.Origins = []string{"remote"}
	p := NewMemoryPublisher(mgr, cfg, nil)
	t.Cleanup(p.Close)

	p.Observer()(context.Background(), kb.WriteEvent{Scope: "project/", Origin: "remote"})

	if n := len(sink.seen()); n != 0 {
		t.Fatalf("remote-origin event republished (%d events)", n)
	}
}

func TestPublisherMarksDryRun(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	cfg := syncCfg("project/")
	cfg.DryRun = true
	p := NewMemoryPublisher(mgr, cfg, nil)
	t.Cleanup(p.Close)

	p.Observer()(context.Background(), kb.WriteEvent{Scope: "project/"})

	got := sink.seen()
	if len(got) != 1 || !got[0].DryRun {
		t.Fatalf("dry run not propagated: %+v", got)
	}
}

func TestPublisherContainsPanicAndError(t *testing.T) {
	bad := &sinkExt{baseExt: baseExt{id: "memory.sink.bad"}, panics: true}
	failing := &sinkExt{baseExt: baseExt{id: "memory.sink.failing"}, err: errors.New("nope")}
	good := &sinkExt{baseExt: baseExt{id: "memory.sink.good"}}
	mgr := managerWith(t, bad, failing, good)
	p := NewMemoryPublisher(mgr, syncCfg("project/"), nil)
	t.Cleanup(p.Close)

	p.Observer()(context.Background(), kb.WriteEvent{Scope: "project/"})

	if n := len(good.seen()); n != 1 {
		t.Fatalf("a panicking sink cost another sink its event (%d)", n)
	}
	if got := p.Stats().Failed; got != 2 {
		t.Fatalf("failed = %d, want 2", got)
	}
}

func TestPublisherAsyncDeliversAndDrains(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	p := NewMemoryPublisher(mgr, config.ExtensionsMemoryConfig{
		Enabled: true, Scopes: []string{"project/"},
	}, nil)
	if p == nil {
		t.Fatal("publisher not built")
	}

	for range 5 {
		p.Observer()(context.Background(), kb.WriteEvent{Scope: "project/"})
	}
	p.Close()

	if n := len(sink.seen()); n != 5 {
		t.Fatalf("events = %d, want 5 after drain", n)
	}
}

func TestPublisherCloseIsIdempotent(t *testing.T) {
	sink := &sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}
	mgr := managerWith(t, sink)
	p := NewMemoryPublisher(mgr, syncCfg("project/"), nil)
	p.Close()
	p.Close()

	var nilPub *MemoryPublisher
	nilPub.Close()
	if got := nilPub.Stats().Sinks; got != 0 {
		t.Fatalf("nil publisher stats = %d", got)
	}
}

func TestMemoryStatusReportsSinks(t *testing.T) {
	reporter := &reportingSink{sinkExt: sinkExt{baseExt: baseExt{id: "memory.sink.corp"}}}
	reporter.status = extension.MemorySyncStatus{
		Active: true, Destination: "remembrances.corp.internal",
		Sent: 3, LastSyncAt: time.Now(),
	}
	silent := &sinkExt{baseExt: baseExt{id: "memory.sink.silent"}}
	mgr := managerWith(t, reporter, silent)
	cfg := syncCfg("project/")
	p := NewMemoryPublisher(mgr, cfg, nil)
	t.Cleanup(p.Close)

	st := MemoryStatusOf(mgr, cfg, p)
	if !st.Enabled || !st.Active {
		t.Fatalf("status = %+v", st)
	}
	if len(st.Sinks) != 2 {
		t.Fatalf("sinks = %d, want 2", len(st.Sinks))
	}
	var reported, unreported MemorySinkStatus
	for _, s := range st.Sinks {
		if s.ID == "memory.sink.corp" {
			reported = s
		} else {
			unreported = s
		}
	}
	if !reported.Reports || reported.Destination != "remembrances.corp.internal" || reported.Sent != 3 {
		t.Fatalf("reported sink = %+v", reported)
	}
	// A sink that ships without reporting must be visible as such, not as an
	// idle row indistinguishable from one that does nothing.
	if unreported.Reports {
		t.Fatalf("silent sink claimed to report: %+v", unreported)
	}
}

func TestMemoryStatusOnStandardBuild(t *testing.T) {
	st := MemoryStatusOf(nil, config.ExtensionsMemoryConfig{}, nil)
	if st.Enabled || st.Active {
		t.Fatalf("status = %+v", st)
	}
	if st.Sinks == nil {
		t.Fatal("sinks is nil; the UI has to special-case it")
	}
}
