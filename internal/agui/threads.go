package agui

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// threadStore maps AG-UI thread ids to Pando session ids.
//
// AG-UI threads are owned by the browser: the client keeps its own transcript
// and reuses the same threadId across reloads and restarts. Keeping the mapping
// only in memory (as P1 did) meant a Pando restart silently started a new
// session for a thread the user still considered open, losing the agent-side
// history the client cannot resend (summaries, tool results, compaction state).
//
// The map is therefore write-through to the adapter-owned agui_threads table
// when a database is available. The in-memory map stays authoritative for the
// hot path, so a database that is read-only (a secondary instance) or that
// predates the migration degrades to exactly the P1 behaviour instead of
// failing a run.
type threadStore struct {
	mu sync.RWMutex
	m  map[string]string

	db *sql.DB
	// degraded is set after the first persistence failure so a broken or absent
	// table produces one warning instead of one per request. It is read on every
	// call from arbitrary goroutines, hence the atomic.
	degraded atomic.Bool
}

// threadStoreTimeout bounds the adapter's own queries. They are single-row
// lookups on a primary key: anything slower means the database is contended,
// and the in-memory map is a correct fallback.
const threadStoreTimeout = 2 * time.Second

func newThreadStore(db *sql.DB) *threadStore {
	return &threadStore{m: make(map[string]string), db: db}
}

// get returns the session bound to a thread, consulting the database only when
// the process has not seen the thread yet.
func (t *threadStore) get(ctx context.Context, threadID string) (string, bool) {
	t.mu.RLock()
	id, ok := t.m[threadID]
	t.mu.RUnlock()
	if ok {
		return id, true
	}

	id, ok = t.loadFromDB(ctx, threadID)
	if !ok {
		return "", false
	}
	t.mu.Lock()
	t.m[threadID] = id
	t.mu.Unlock()
	return id, true
}

// put binds a thread to a session, replacing any previous binding.
func (t *threadStore) put(ctx context.Context, threadID, sessionID, agent string) {
	t.mu.Lock()
	t.m[threadID] = sessionID
	t.mu.Unlock()
	t.saveToDB(ctx, threadID, sessionID, agent)
}

// forget drops a binding whose session no longer exists.
func (t *threadStore) forget(ctx context.Context, threadID string) {
	t.mu.Lock()
	delete(t.m, threadID)
	t.mu.Unlock()

	if t.db == nil || t.degraded.Load() {
		return
	}
	qctx, cancel := context.WithTimeout(detach(ctx), threadStoreTimeout)
	defer cancel()
	if _, err := t.db.ExecContext(qctx, `DELETE FROM agui_threads WHERE thread_id = ?`, threadID); err != nil {
		t.degrade("delete", err)
	}
}

func (t *threadStore) loadFromDB(ctx context.Context, threadID string) (string, bool) {
	if t.db == nil || t.degraded.Load() {
		return "", false
	}
	qctx, cancel := context.WithTimeout(detach(ctx), threadStoreTimeout)
	defer cancel()

	var sessionID string
	err := t.db.QueryRowContext(qctx,
		`SELECT session_id FROM agui_threads WHERE thread_id = ?`, threadID).Scan(&sessionID)
	switch {
	case err == sql.ErrNoRows:
		return "", false
	case err != nil:
		t.degrade("lookup", err)
		return "", false
	}
	return sessionID, sessionID != ""
}

func (t *threadStore) saveToDB(ctx context.Context, threadID, sessionID, agent string) {
	if t.db == nil || t.degraded.Load() {
		return
	}
	qctx, cancel := context.WithTimeout(detach(ctx), threadStoreTimeout)
	defer cancel()

	_, err := t.db.ExecContext(qctx, `
		INSERT INTO agui_threads (thread_id, session_id, agent)
		VALUES (?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			session_id = excluded.session_id,
			agent      = excluded.agent,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		threadID, sessionID, agent)
	if err != nil {
		t.degrade("persist", err)
	}
}

// degrade turns off persistence for the rest of the process's life. A read-only
// connection or a database that predates the migration must not turn every run
// into an error: the adapter keeps working with the in-memory map.
func (t *threadStore) degrade(op string, err error) {
	if t.degraded.Swap(true) {
		return
	}
	logging.Warn("agui: thread mapping is memory-only for this process",
		"operation", op, "error", err)
}

// detach strips a cancellation that belongs to the caller's request. Persisting
// the binding must not be skipped just because the browser hung up while the
// session was being created; the values are still needed by the next request.
func detach(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}
