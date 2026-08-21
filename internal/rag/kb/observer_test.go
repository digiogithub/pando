package kb

import (
	"context"
	"sync"
	"testing"
)

// recorder collects the writes the store publishes.
type recorder struct {
	mu     sync.Mutex
	events []WriteEvent
}

func (r *recorder) observe(_ context.Context, ev WriteEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) seen() []WriteEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WriteEvent(nil), r.events...)
}

func observedStore(t *testing.T) (*KBStore, *recorder) {
	t.Helper()
	db := openTestKBDB(t)
	// ":memory:" gives every pooled connection its own empty database, so a
	// second connection would not see the schema this helper created.
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	rec := &recorder{}
	store.SetWriteObserver(rec.observe)
	return store, rec
}

func TestWriteObserverSeesDocumentLifecycle(t *testing.T) {
	store, rec := observedStore(t)
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/a.md", "first", map[string]any{"tags": []string{"x"}}); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	if err := store.UpdateDocument(ctx, "docs/a.md", "second", nil); err != nil {
		t.Fatalf("UpdateDocument() error = %v", err)
	}
	if err := store.DeleteDocument(ctx, "docs/a.md"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}

	got := rec.seen()
	if len(got) != 3 {
		t.Fatalf("events = %d, want 3", len(got))
	}
	want := []WriteOp{WriteCreated, WriteUpdated, WriteDeleted}
	for i, op := range want {
		if got[i].Op != op {
			t.Fatalf("event %d op = %q, want %q", i, got[i].Op, op)
		}
		if got[i].Kind != WriteKindDocument {
			t.Fatalf("event %d kind = %q", i, got[i].Kind)
		}
		if got[i].FilePath != "docs/a.md" {
			t.Fatalf("event %d path = %q", i, got[i].FilePath)
		}
		if got[i].Timestamp.IsZero() {
			t.Fatalf("event %d has no timestamp", i)
		}
	}
	if len(got[0].Tags) != 1 || got[0].Tags[0] != "x" {
		t.Fatalf("tags = %v, want [x]", got[0].Tags)
	}
	if got[0].Origin != "tool" {
		t.Fatalf("default origin = %q, want tool", got[0].Origin)
	}
}

// A failed write must publish nothing: an observer that can report a write
// that did not happen is worse than one that reports nothing at all.
func TestWriteObserverSilentOnFailure(t *testing.T) {
	store, rec := observedStore(t)
	ctx := context.Background()

	if err := store.AddDocument(ctx, "", "body", nil); err == nil {
		t.Fatal("AddDocument() with an empty path succeeded")
	}
	if n := len(rec.seen()); n != 0 {
		t.Fatalf("events = %d after a failed write, want 0", n)
	}
}

// UpsertMemory delegates to AddDocument/UpdateDocument on the keyless path.
// The write must be reported once, as a memory, with its scope and key.
func TestWriteObserverMemoryUpsertReportedOnce(t *testing.T) {
	store, rec := observedStore(t)
	ctx := context.Background()

	opts := MemoryUpsertOptions{
		FilePath: "memory/project/a.md",
		Content:  "fact",
		Scope:    "project/",
		Tags:     []string{"memory"},
	}
	if _, err := store.UpsertMemory(ctx, opts); err != nil {
		t.Fatalf("UpsertMemory() error = %v", err)
	}

	got := rec.seen()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 (the nested document write must be suppressed)", len(got))
	}
	if got[0].Kind != WriteKindMemory || got[0].Op != WriteCreated {
		t.Fatalf("event = %+v", got[0])
	}
	if got[0].Scope != "project/" {
		t.Fatalf("scope = %q", got[0].Scope)
	}
}

// An upsert by key lands on the path the memory already has, not on the one
// the caller passed.
func TestWriteObserverMemoryUpsertByKeyUsesStoredPath(t *testing.T) {
	store, rec := observedStore(t)
	ctx := context.Background()

	opts := MemoryUpsertOptions{
		FilePath: "memory/project/first.md",
		Key:      "pando.k",
		Scope:    "project/",
		Content:  "one",
	}
	if _, err := store.UpsertMemory(ctx, opts); err != nil {
		t.Fatalf("first UpsertMemory() error = %v", err)
	}
	opts.FilePath = "memory/project/second.md"
	opts.Content = "two"
	if _, err := store.UpsertMemory(ctx, opts); err != nil {
		t.Fatalf("second UpsertMemory() error = %v", err)
	}

	got := rec.seen()
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	if got[1].Op != WriteUpdated {
		t.Fatalf("second op = %q, want updated", got[1].Op)
	}
	if got[1].FilePath != "memory/project/first.md" {
		t.Fatalf("second path = %q, want the stored path", got[1].FilePath)
	}
	if got[1].Key != "pando.k" {
		t.Fatalf("key = %q", got[1].Key)
	}
}

func TestWriteOriginAndSuppression(t *testing.T) {
	store, rec := observedStore(t)

	if err := store.AddDocument(WithWriteOrigin(context.Background(), "sync"), "docs/a.md", "x", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	if err := store.AddDocument(WithoutWriteObserver(context.Background()), "docs/b.md", "x", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	got := rec.seen()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	if got[0].Origin != "sync" {
		t.Fatalf("origin = %q, want sync", got[0].Origin)
	}
}

func TestSearchMiddlewareWrapsStoreSearch(t *testing.T) {
	db := openTestKBDB(t)
	db.SetMaxOpenConns(1)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()
	if err := store.AddDocument(ctx, "docs/a.md", "alpha beta", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	called := false
	store.SetSearchMiddleware(func(ctx context.Context, query string, limit int, opts SearchOptions, next SearchNext) ([]SearchResult, error) {
		called = true
		results, err := next(ctx, query, limit, opts)
		if err != nil {
			return nil, err
		}
		return append(results, SearchResult{
			Document:     Document{FilePath: "corp/x.md"},
			ChunkContent: "remote",
			Score:        1,
		}), nil
	})

	got, err := store.SearchDocumentsWithOptions(ctx, "alpha", 5, SearchOptions{})
	if err != nil {
		t.Fatalf("SearchDocumentsWithOptions() error = %v", err)
	}
	if !called {
		t.Fatal("middleware not called")
	}
	if len(got) == 0 || got[len(got)-1].Document.FilePath != "corp/x.md" {
		t.Fatalf("middleware result not returned: %+v", got)
	}

	store.SetSearchMiddleware(nil)
	if _, err := store.SearchDocumentsWithOptions(ctx, "alpha", 5, SearchOptions{}); err != nil {
		t.Fatalf("search after removing middleware: %v", err)
	}
}
