package events

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// openConcurrencyTestDB opens a temp-file SQLite database (not ":memory:" —
// a real file is required so several real connections in the pool see the
// same database and can genuinely contend with each other) with the given
// _txlock mode. The DSN parameters used here ("_txlock" and
// "_pragma=busy_timeout(...)") are the ncruces/go-sqlite3 driver's own
// documented DSN convention (see that package's driver doc comment) — the
// same mechanism internal/db.buildDSN + openPool wire up for production,
// spelled out inline here so this test does not need access to that
// package's unexported helpers.
//
// withSessionIndex controls whether idx_events_session (added by migration
// 20260911000001_add_events_session_index.sql) exists. The "before" test
// below deliberately omits it, matching the pre-fix production schema where
// the session lookup was a full scan of the events table filtered by
// json_extract — that scan is what widened the vulnerable read-then-write
// window enough to reproduce the bug reliably in a short test run.
func openConcurrencyTestDB(t *testing.T, txLock string, busyTimeoutMS int, withSessionIndex bool) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)", path, busyTimeoutMS)
	if txLock != "" {
		dsn += "&_txlock=" + txLock
	}

	dbConn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })
	dbConn.SetMaxOpenConns(4)

	if _, err := dbConn.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		t.Fatalf("set journal_mode: %v", err)
	}

	schema := `
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
	CREATE TABLE autocommit_writes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		value INTEGER NOT NULL
	);
	`
	if _, err := dbConn.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	if withSessionIndex {
		if _, err := dbConn.Exec(`CREATE INDEX idx_events_session ON events(subject, json_extract(metadata, '$.session_id'));`); err != nil {
			t.Fatalf("create idx_events_session: %v", err)
		}
	}

	return dbConn
}

// seedBulkSessionEvents inserts sessions*perSession rows spread across many
// unrelated sessions, so the events table is a few-thousand-row table — the
// size that made deleteSessionEventsTx's read a real, timeable scan in
// production before idx_events_session existed.
func seedBulkSessionEvents(t *testing.T, dbConn *sql.DB, sessions, perSession int) {
	t.Helper()
	now := time.Now().UTC()
	stmt, err := dbConn.Prepare(`INSERT INTO events (subject, content, metadata, event_at, created_at) VALUES ('session', ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare seed insert: %v", err)
	}
	defer stmt.Close()

	for s := 0; s < sessions; s++ {
		meta := fmt.Sprintf(`{"session_id":"seed-%d"}`, s)
		for i := 0; i < perSession; i++ {
			if _, err := stmt.Exec(fmt.Sprintf("seed chunk %d-%d", s, i), meta, now, now); err != nil {
				t.Fatalf("seed insert: %v", err)
			}
		}
	}
}

// isLockError reports whether err is a SQLite lock-contention error of the
// kind this fix removes: "database is locked" (SQLITE_BUSY) or the WAL
// "database is locked" variant produced by a stale snapshot
// (SQLITE_BUSY_SNAPSHOT). Both render through the ncruces driver with
// "database is locked" in the message.
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(strings.ToLower(msg), "locked")
}

// runConcurrentReplace runs goroutine A (plain autocommit inserts on an
// unrelated table, as fast as possible) concurrently with goroutine B
// (iterations rounds of EventStore.ReplaceSessionEvents on one "hot"
// session — the exact read-then-write shape of ReplaceSessionEvents /
// deleteSessionEventsTx), and returns how many of B's calls failed with a
// SQLite lock error.
func runConcurrentReplace(t *testing.T, dbConn *sql.DB, iterations int) int64 {
	t.Helper()
	store := NewEventStore(dbConn, nil)
	ctx := context.Background()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var lockFailures int64

	// Goroutine A: unrelated autocommit writes, to maximize the chance a
	// COMMIT from another connection lands inside B's read-then-write
	// window.
	wg.Add(1)
	go func() {
		defer wg.Done()
		n := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = dbConn.ExecContext(ctx, `INSERT INTO autocommit_writes (value) VALUES (?)`, n)
			n++
		}
	}()

	// Goroutine B: repeatedly replace the "hot" session's indexed chunks.
	var bErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)

		chunks := []string{"chunk one", "chunk two", "chunk three"}
		embeds := [][]float32{{1, 1}, {2, 2}, {3, 3}}
		for i := 0; i < iterations; i++ {
			meta := map[string]interface{}{"session_id": "hot-session", "iteration": i}
			if err := store.ReplaceSessionEvents(ctx, "hot-session", "session", meta, chunks, embeds); err != nil {
				if isLockError(err) {
					atomic.AddInt64(&lockFailures, 1)
					continue
				}
				bErr = err
				return
			}
		}
	}()

	wg.Wait()
	if bErr != nil {
		t.Fatalf("ReplaceSessionEvents() unexpected non-lock error = %v", bErr)
	}
	return lockFailures
}

// TestReplaceSessionEventsConcurrentWritesNoLockErrors is the "quick SQLite
// wins" regression test: with _txlock=immediate (what internal/db.Connect
// now sets on every write connection) and a real busy_timeout, a
// read-then-write ReplaceSessionEvents transaction never fails with
// "database is locked" while an unrelated connection commits concurrently —
// it either succeeds immediately (no contention) or waits out the busy
// handler, but the read-then-write upgrade never gets skipped past the
// busy handler the way a DEFERRED transaction's does.
func TestReplaceSessionEventsConcurrentWritesNoLockErrors(t *testing.T) {
	dbConn := openConcurrencyTestDB(t, "immediate", 5000, true)
	seedBulkSessionEvents(t, dbConn, 30, 100) // ~3000 unrelated rows

	if failures := runConcurrentReplace(t, dbConn, 200); failures != 0 {
		t.Fatalf("got %d lock failures with _txlock=immediate, want 0", failures)
	}
}

// TestReplaceSessionEventsConcurrentWritesDeferredFails demonstrates the bug
// this fix removes: with the driver's default DEFERRED transactions (no
// _txlock override) and the pre-fix schema (no idx_events_session, so the
// session lookup scans the whole table), the exact same workload reliably
// produces "database is locked" errors — SQLite never invokes the busy
// handler when a DEFERRED transaction upgrades from a read lock to a write
// lock mid-transaction, so a concurrent commit during the scan fails
// instantly instead of waiting.
//
// This reproduction is inherently timing-dependent (it needs a commit from
// goroutine A to land inside a scan of a few milliseconds), so a run that
// happens not to reproduce it is skipped rather than failed; see
// TestReplaceSessionEventsConcurrentWritesNoLockErrors for the assertion
// that actually gates the fix.
func TestReplaceSessionEventsConcurrentWritesDeferredFails(t *testing.T) {
	dbConn := openConcurrencyTestDB(t, "", 5000, false) // no _txlock override => deferred, no session index
	seedBulkSessionEvents(t, dbConn, 30, 100)

	failures := runConcurrentReplace(t, dbConn, 200)
	if failures == 0 {
		t.Skip("did not reproduce the deferred-transaction lock bug in this run (timing-dependent)")
	}
	t.Logf("reproduced %d lock failures with the deferred, pre-fix transaction mode, as expected", failures)
}

// TestSessionEventsQueryUsesExpressionIndex asserts that the exact query
// deleteSessionEventsTx runs (events.go) is matched by idx_events_session —
// the migration and the query must keep using the identical expression
// (same function, same JSON path literal) for SQLite to pick the index.
func TestSessionEventsQueryUsesExpressionIndex(t *testing.T) {
	dbConn := openTestEventStoreDB(t) // includes idx_events_session

	rows, err := dbConn.QueryContext(context.Background(), `
		EXPLAIN QUERY PLAN
		SELECT id, subject, content
		FROM events
		WHERE subject = ? AND json_extract(metadata, '$.session_id') = ?`,
		"session", "any-session",
	)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}

	if !strings.Contains(plan.String(), "idx_events_session") {
		t.Fatalf("expected query plan to use idx_events_session, got:\n%s", plan.String())
	}
}
