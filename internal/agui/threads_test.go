package agui

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// newThreadDB builds an in-memory database carrying the same schema as the
// agui_threads migration.
func newThreadDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE agui_threads (
			thread_id  TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			agent      TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// TestThreadStoreSurvivesRestart is the point of P5: a second process (here, a
// second store over the same database) must resolve a thread the first one
// bound, instead of silently starting a new session.
func TestThreadStoreSurvivesRestart(t *testing.T) {
	db := newThreadDB(t)
	ctx := context.Background()

	first := newThreadStore(db)
	first.put(ctx, "thread-1", "session-1", "coder")

	second := newThreadStore(db)
	got, ok := second.get(ctx, "thread-1")
	if !ok || got != "session-1" {
		t.Fatalf("get = %q, %v; want session-1", got, ok)
	}
}

func TestThreadStoreRebindsAndForgets(t *testing.T) {
	db := newThreadDB(t)
	ctx := context.Background()
	store := newThreadStore(db)

	store.put(ctx, "thread-1", "session-1", "coder")
	store.put(ctx, "thread-1", "session-2", "coder")
	if got, _ := newThreadStore(db).get(ctx, "thread-1"); got != "session-2" {
		t.Fatalf("rebinding was not persisted, got %q", got)
	}

	store.forget(ctx, "thread-1")
	if _, ok := store.get(ctx, "thread-1"); ok {
		t.Fatal("a forgotten thread must not resolve")
	}
	if _, ok := newThreadStore(db).get(ctx, "thread-1"); ok {
		t.Fatal("forget must also delete the row")
	}
}

// TestThreadStorePersistsWithCancelledContext: the browser hanging up mid-run
// must not cost the binding, which the *next* request needs.
func TestThreadStorePersistsWithCancelledContext(t *testing.T) {
	db := newThreadDB(t)
	store := newThreadStore(db)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store.put(ctx, "thread-1", "session-1", "coder")

	if got, ok := newThreadStore(db).get(context.Background(), "thread-1"); !ok || got != "session-1" {
		t.Fatalf("binding was lost with the request context: %q %v", got, ok)
	}
}

// TestThreadStoreWithoutDatabase pins the degraded mode: no database is a
// supported configuration (the P1 behaviour), never an error.
func TestThreadStoreWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	store := newThreadStore(nil)

	store.put(ctx, "thread-1", "session-1", "coder")
	if got, ok := store.get(ctx, "thread-1"); !ok || got != "session-1" {
		t.Fatalf("the in-memory map must still work: %q %v", got, ok)
	}
	store.forget(ctx, "thread-1")
	if _, ok := store.get(ctx, "thread-1"); ok {
		t.Fatal("forget must drop the in-memory entry")
	}
}

// TestThreadStoreDegradesOnBrokenSchema: a database that predates the migration
// (or a read-only secondary) must not turn every request into a failure.
func TestThreadStoreDegradesOnBrokenSchema(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	store := newThreadStore(db)
	store.put(ctx, "thread-1", "session-1", "coder")

	if !store.degraded.Load() {
		t.Fatal("a failing write must switch the store to memory-only")
	}
	if got, ok := store.get(ctx, "thread-1"); !ok || got != "session-1" {
		t.Fatalf("the run must keep working from memory: %q %v", got, ok)
	}
}
