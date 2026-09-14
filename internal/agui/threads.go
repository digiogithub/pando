package agui

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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

// threadRecord is one row of the adapter-owned thread list (PANDO-US-0015).
// It is the threadStore-level shape; server.go's handleListThreads projects
// it into the wire-level ThreadSummary.
type threadRecord struct {
	ThreadID  string
	SessionID string
	Agent     string
	UpdatedAt string
}

// list returns up to limit threads owned by this adapter, newest-first by
// updated_at, starting at offset. It reads only the agui_threads table -- the
// in-memory map has no ordering of its own and is never consulted here, so a
// database-less (degraded) adapter simply has no threads to list, the same
// shape get/put/forget already degrade to. Because agui_threads is a table
// this package alone writes to, every row it returns belongs to this adapter:
// there is no need (and, per the story, no fallback allowed) to reconstruct
// the list from session titles.
func (t *threadStore) list(ctx context.Context, limit, offset int) ([]threadRecord, error) {
	if t.db == nil || t.degraded.Load() {
		return nil, nil
	}
	qctx, cancel := context.WithTimeout(detach(ctx), threadStoreTimeout)
	defer cancel()

	rows, err := t.db.QueryContext(qctx, `
		SELECT thread_id, session_id, agent, updated_at
		FROM agui_threads
		ORDER BY updated_at DESC, thread_id DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		t.degrade("list", err)
		return nil, err
	}
	defer rows.Close()

	out := make([]threadRecord, 0, limit)
	for rows.Next() {
		var rec threadRecord
		if err := rows.Scan(&rec.ThreadID, &rec.SessionID, &rec.Agent, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
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

// ---------------------------------------------------------------- thread API
//
// PANDO-US-0015: list threads, read one's transcript, delete a thread -- the
// three routes a browser client needs to rebuild a conversation on reload
// without co-mounting the Web-UI REST API. They are registered by
// server.go's Register/Handler and go through the same authorize() every
// other route does.

const (
	// defaultThreadPageSize is used when a client sends no ?limit.
	defaultThreadPageSize = 50
	// maxThreadPageSize caps how much a single request can ask for.
	maxThreadPageSize = 200
)

// ThreadSummary is one entry of GET {path}/threads.
type ThreadSummary struct {
	ThreadID  string `json:"threadId"`
	SessionID string `json:"sessionId"`
	Agent     string `json:"agent"`
	UpdatedAt string `json:"updatedAt"`
}

// handleListThreads answers GET {path}/threads: this adapter's threads,
// newest-first, paginated by ?limit=&offset=.
func (r *Runtime) handleListThreads(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}
	limit, offset := threadPaginationParams(req)

	// One extra row is fetched to learn hasMore without a second COUNT(*).
	records, err := r.threads.list(req.Context(), limit+1, offset)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	threads := make([]ThreadSummary, len(records))
	for i, rec := range records {
		threads[i] = ThreadSummary{
			ThreadID:  rec.ThreadID,
			SessionID: rec.SessionID,
			Agent:     rec.Agent,
			UpdatedAt: rec.UpdatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"threads": threads,
		"limit":   limit,
		"offset":  offset,
		"hasMore": hasMore,
	})
}

// threadPaginationParams reads ?limit= and ?offset=. A missing or invalid
// limit falls back to defaultThreadPageSize; limit is capped at
// maxThreadPageSize and offset is never negative.
func threadPaginationParams(req *http.Request) (limit, offset int) {
	limit = defaultThreadPageSize
	if raw := req.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxThreadPageSize {
		limit = maxThreadPageSize
	}
	if raw := req.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			offset = v
		}
	}
	return limit, offset
}

// handleThreadMessages answers GET {path}/threads/{id}/messages: the thread's
// transcript, AG-UI Message[] shaped via toAGUIMessages (transcript.go). An
// id this adapter has no binding for answers 404, never an empty transcript:
// there would be no way to tell "empty conversation" from "never existed"
// apart otherwise.
func (r *Runtime) handleThreadMessages(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}
	threadID := strings.TrimSpace(req.PathValue("id"))
	sessionID, ok := r.threads.get(req.Context(), threadID)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "thread not found")
		return
	}

	msgs, err := r.deps.Messages.List(req.Context(), sessionID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"threadId": threadID,
		"messages": toAGUIMessages(msgs),
	})
}

// handleDeleteThread answers DELETE {path}/threads/{id}: it removes the
// thread's messages, its session and the agui_threads binding itself, so a
// subsequent GET on the same id 404s and a subsequent run on it starts a
// fresh session. It is idempotent: an id this adapter has no binding for --
// never bound, or already deleted -- is a no-op success, not an error,
// exactly like a repeat DELETE of the same thread.
func (r *Runtime) handleDeleteThread(w http.ResponseWriter, req *http.Request) {
	if !r.authorize(w, req) {
		return
	}
	threadID := strings.TrimSpace(req.PathValue("id"))
	ctx := req.Context()

	sessionID, ok := r.threads.get(ctx, threadID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// A run still in flight for this thread must not be left dangling on a
	// session that is about to disappear out from under it.
	if run, ok := r.runs.get(threadID); ok {
		r.finishRun(run)
	}
	// The thread's shared-state document belongs to the conversation being
	// deleted; a fresh run on the same thread id must start with a clean one
	// rather than inheriting its predecessor's todos/files/sub-agents.
	r.states.delete(threadID)

	if err := r.deps.Messages.DeleteSessionMessages(ctx, sessionID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := r.deps.Sessions.Delete(ctx, sessionID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The existing dangling-binding recovery (Runtime.sessionForThread) stays
	// the safety net for a session deleted through some other path; forgetting
	// the binding here is what makes THIS deletion observable immediately, on
	// both the in-memory map and the durable table, instead of waiting for
	// that recovery to notice on the next run.
	r.threads.forget(ctx, threadID)

	w.WriteHeader(http.StatusNoContent)
}
