package app

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/session"
)

func openCleanupTestEventsDB(t *testing.T) *sql.DB {
	t.Helper()
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	migration := `
	CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subject TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT NOT NULL DEFAULT '{}',
		embedding BLOB,
		event_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);
	CREATE INDEX idx_events_subject ON events(subject);
	CREATE VIRTUAL TABLE events_fts USING fts5(
		subject,
		content,
		content='events',
		content_rowid='id'
	);
	`
	if _, err := dbConn.Exec(migration); err != nil {
		t.Fatalf("db.Exec(migration) error = %v", err)
	}
	return dbConn
}

func countSessionEvents(t *testing.T, db *sql.DB, sessionID string) int {
	t.Helper()
	var count int
	row := db.QueryRow(`SELECT COUNT(*) FROM events WHERE json_extract(metadata, '$.session_id') = ?`, sessionID)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count session events for %q: %v", sessionID, err)
	}
	return count
}

// TestCleanupLeftoverEnrichmentSessionsRemovesSessionsAndIndexedEvents covers the
// one-off startup cleanup: it must remove every "ctxenrich-*" session, leave
// unrelated sessions untouched, and also remove whatever the remembrances
// session indexer had written for those sessions before the ctxenrich- skip
// filter existed.
func TestCleanupLeftoverEnrichmentSessionsRemovesSessionsAndIndexedEvents(t *testing.T) {
	sessions := newEnricherFakeSessions()
	ctx := context.Background()

	leftover1, err := sessions.CreateTaskSession(ctx, "ctxenrich-aaa", "parent-1", "Context enrichment")
	if err != nil {
		t.Fatalf("seed leftover1: %v", err)
	}
	leftover2, err := sessions.CreateTaskSession(ctx, "ctxenrich-bbb", "parent-1", "Context enrichment")
	if err != nil {
		t.Fatalf("seed leftover2: %v", err)
	}
	kept, err := sessions.Create(ctx, "A real chat session")
	if err != nil {
		t.Fatalf("seed kept session: %v", err)
	}

	db := openCleanupTestEventsDB(t)
	eventStore := events.NewEventStore(db, nil)
	for _, id := range []string{leftover1.ID, leftover2.ID, kept.ID} {
		meta := map[string]interface{}{"session_id": id}
		if err := eventStore.ReplaceSessionEvents(ctx, id, sessionIndexSubject, meta, []string{"chunk"}, [][]float32{{1, 2}}); err != nil {
			t.Fatalf("seed indexed event for %q: %v", id, err)
		}
	}

	app := &App{
		Sessions:     sessions,
		Remembrances: &rag.RemembrancesService{Events: eventStore},
	}

	app.cleanupLeftoverEnrichmentSessions(ctx)

	remaining, err := sessions.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	remainingIDs := map[string]bool{}
	for _, s := range remaining {
		remainingIDs[s.ID] = true
	}
	if remainingIDs[leftover1.ID] || remainingIDs[leftover2.ID] {
		t.Fatalf("leftover ctxenrich- sessions were not removed: %v", remaining)
	}
	if !remainingIDs[kept.ID] {
		t.Fatal("the unrelated session was removed; it must be kept")
	}

	if got := countSessionEvents(t, db, leftover1.ID); got != 0 {
		t.Fatalf("indexed events for %q = %d, want 0", leftover1.ID, got)
	}
	if got := countSessionEvents(t, db, leftover2.ID); got != 0 {
		t.Fatalf("indexed events for %q = %d, want 0", leftover2.ID, got)
	}
	if got := countSessionEvents(t, db, kept.ID); got != 1 {
		t.Fatalf("indexed events for the kept session = %d, want 1 (untouched)", got)
	}

	// Idempotent: a second run finds nothing left to do and must not error or
	// touch the kept session.
	app.cleanupLeftoverEnrichmentSessions(ctx)
	if got := countSessionEvents(t, db, kept.ID); got != 1 {
		t.Fatalf("second run altered the kept session's indexed events: got %d", got)
	}
}

// TestCleanupLeftoverEnrichmentSessionsNoSessionsIsANoOp guards the nil-safety
// callers rely on (Sessions/Remembrances unset, as on a build without remembrances).
func TestCleanupLeftoverEnrichmentSessionsNoSessionsIsANoOp(t *testing.T) {
	app := &App{}
	app.cleanupLeftoverEnrichmentSessions(context.Background())
}

var _ session.Service = (*enricherFakeSessions)(nil)
