package agui

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
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

// ------------------------------------------------------- threadStore.list

// seedThread inserts a row directly, bypassing put()'s "now" timestamp, so
// ordering tests are not at the mercy of two calls landing in the same
// second.
func seedThread(t *testing.T, db *sql.DB, threadID, sessionID, agent, updatedAt string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO agui_threads (thread_id, session_id, agent, updated_at)
		VALUES (?, ?, ?, ?)`, threadID, sessionID, agent, updatedAt); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
}

// TestThreadStoreListNewestFirstAndPaginated is the PANDO-US-0015 acceptance
// criterion for GET {path}/threads: newest-first ordering and limit/offset
// pagination.
func TestThreadStoreListNewestFirstAndPaginated(t *testing.T) {
	db := newThreadDB(t)
	ctx := context.Background()
	seedThread(t, db, "t1", "s1", "coder", "2024-01-01T00:00:01Z")
	seedThread(t, db, "t2", "s2", "coder", "2024-01-01T00:00:03Z")
	seedThread(t, db, "t3", "s3", "coder", "2024-01-01T00:00:02Z")

	store := newThreadStore(db)

	page1, err := store.list(ctx, 2, 0)
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(page1) != 2 || page1[0].ThreadID != "t2" || page1[1].ThreadID != "t3" {
		t.Fatalf("page1 = %+v, want [t2 t3] newest-first", page1)
	}

	page2, err := store.list(ctx, 2, 2)
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2) != 1 || page2[0].ThreadID != "t1" {
		t.Fatalf("page2 = %+v, want [t1]", page2)
	}
}

// TestThreadStoreListWithoutDatabaseReturnsEmpty pins the degraded-mode shape:
// a database-less (or degraded) adapter has no ordered history to list from,
// so list must return an empty result rather than erroring the route.
func TestThreadStoreListWithoutDatabaseReturnsEmpty(t *testing.T) {
	got, err := newThreadStore(nil).list(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no threads without a database, got %v", got)
	}
}

// -------------------------------------------------- thread-API HTTP routes

// fakeSessionService is a minimal in-memory session.Service for the thread-API
// tests: it behaves realistically for what threads.go's handlers touch
// (Create, Get, Delete); the rest of the interface is satisfied but unused.
type fakeSessionService struct {
	*pubsub.Broker[session.Session]
	mu       sync.Mutex
	sessions map[string]session.Session
	nextID   int
}

func newFakeSessionService() *fakeSessionService {
	return &fakeSessionService{
		Broker:   pubsub.NewBroker[session.Session](),
		sessions: make(map[string]session.Session),
	}
}

func (f *fakeSessionService) Create(_ context.Context, title string) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	sess := session.Session{ID: fmt.Sprintf("sess-%d", f.nextID), Title: title}
	f.sessions[sess.ID] = sess
	return sess, nil
}

func (f *fakeSessionService) CreateTitleSession(context.Context, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) CreateTaskSession(context.Context, string, string, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) GetACPSessionState(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeSessionService) Get(_ context.Context, id string) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[id]
	if !ok {
		return session.Session{}, sql.ErrNoRows
	}
	return sess, nil
}

func (f *fakeSessionService) List(context.Context) ([]session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]session.Session, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSessionService) SaveACPSessionState(context.Context, string, string) error { return nil }

func (f *fakeSessionService) Save(_ context.Context, sess session.Session) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[sess.ID] = sess
	return sess, nil
}

func (f *fakeSessionService) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.sessions[id]; !ok {
		return sql.ErrNoRows
	}
	delete(f.sessions, id)
	return nil
}

func (f *fakeSessionService) EndSession(context.Context, string) error { return nil }

// fakeMessageService is a minimal in-memory message.Service for the
// thread-API tests.
type fakeMessageService struct {
	*pubsub.Broker[message.Message]
	mu        sync.Mutex
	bySession map[string][]message.Message
	nextID    int
}

func newFakeMessageService() *fakeMessageService {
	return &fakeMessageService{
		Broker:    pubsub.NewBroker[message.Message](),
		bySession: make(map[string][]message.Message),
	}
}

// seed appends ready-made messages directly, so a test can control ids and
// tool call/result pairing precisely instead of going through Create's
// synthetic Finish-part behaviour.
func (f *fakeMessageService) seed(sessionID string, msgs ...message.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bySession[sessionID] = append(f.bySession[sessionID], msgs...)
}

func (f *fakeMessageService) Create(_ context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	msg := message.Message{
		ID:        fmt.Sprintf("msg-%d", f.nextID),
		SessionID: sessionID,
		Role:      params.Role,
		Parts:     params.Parts,
	}
	f.bySession[sessionID] = append(f.bySession[sessionID], msg)
	return msg, nil
}

func (f *fakeMessageService) Update(_ context.Context, msg message.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.bySession[msg.SessionID] {
		if m.ID == msg.ID {
			f.bySession[msg.SessionID][i] = msg
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeMessageService) Get(_ context.Context, id string) (message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, msgs := range f.bySession {
		for _, m := range msgs {
			if m.ID == id {
				return m, nil
			}
		}
	}
	return message.Message{}, sql.ErrNoRows
}

func (f *fakeMessageService) List(_ context.Context, sessionID string) ([]message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]message.Message, len(f.bySession[sessionID]))
	copy(out, f.bySession[sessionID])
	return out, nil
}

func (f *fakeMessageService) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sid, msgs := range f.bySession {
		for i, m := range msgs {
			if m.ID == id {
				f.bySession[sid] = append(msgs[:i:i], msgs[i+1:]...)
				return nil
			}
		}
	}
	return sql.ErrNoRows
}

func (f *fakeMessageService) DeleteSessionMessages(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.bySession, sessionID)
	return nil
}

// newThreadTestRuntime builds a Runtime with a DB-backed threadStore and fake
// Sessions/Messages services -- enough to exercise the thread-API handlers
// (and, via r.perms, sessionForThread) end to end without a real agent pool.
func newThreadTestRuntime(t *testing.T, db *sql.DB) (*Runtime, *fakeSessionService, *fakeMessageService) {
	t.Helper()
	sessions := newFakeSessionService()
	messages := newFakeMessageService()
	r := &Runtime{
		deps: Deps{
			Sessions: sessions,
			Messages: messages,
			DB:       db,
			Token:    "secret",
		},
		cfg:     testConfig(),
		perms:   permission.NewPermissionService(),
		threads: newThreadStore(db),
		states:  newStateStore(),
		pending: newPendingRegistry(),
		runs:    newRunStore(),
		baseCtx: context.Background(),
	}
	return r, sessions, messages
}

// TestThreadRoutesRequireAuthorizeOnDedicatedListener is the PANDO-US-0015
// acceptance criterion that all three routes go through authorize() and
// answer correctly on r.Handler(), the mux the dedicated agui-serve listener
// (which carries no REST API) actually serves.
func TestThreadRoutesRequireAuthorizeOnDedicatedListener(t *testing.T) {
	db := newThreadDB(t)
	r, _, _ := newThreadTestRuntime(t, db)
	mux := r.Handler()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, defaultPath+"/threads", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without a token", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, defaultPath+"/threads", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleListThreadsNewestFirstPaginated is the PANDO-US-0015 acceptance
// criterion for GET {path}/threads served through the real handler.
func TestHandleListThreadsNewestFirstPaginated(t *testing.T) {
	db := newThreadDB(t)
	r, _, _ := newThreadTestRuntime(t, db)
	seedThread(t, db, "t1", "s1", "coder", "2024-01-01T00:00:01Z")
	seedThread(t, db, "t2", "s2", "coder", "2024-01-01T00:00:02Z")

	req := httptest.NewRequest(http.MethodGet, defaultPath+"/threads?limit=1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	r.handleListThreads(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Threads []ThreadSummary `json:"threads"`
		HasMore bool            `json:"hasMore"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Threads) != 1 || resp.Threads[0].ThreadID != "t2" {
		t.Fatalf("expected the newest thread t2 first: %+v", resp.Threads)
	}
	if !resp.HasMore {
		t.Fatal("expected hasMore=true with a second page pending")
	}
}

// TestHandleThreadMessagesConvertsToAGUIShape is the PANDO-US-0015 acceptance
// criterion: AG-UI-shaped messages, toolCalls/toolCallId preserved, and
// assistant/tool pairing intact.
func TestHandleThreadMessagesConvertsToAGUIShape(t *testing.T) {
	db := newThreadDB(t)
	r, sessions, messages := newThreadTestRuntime(t, db)
	ctx := context.Background()

	sess, err := sessions.Create(ctx, "agui: test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	r.threads.put(ctx, "t1", sess.ID, "coder")

	messages.seed(sess.ID,
		message.Message{ID: "m1", SessionID: sess.ID, Role: message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		message.Message{ID: "m2", SessionID: sess.ID, Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: "let me check"},
				message.ToolCall{ID: "call-1", Name: "view", Input: `{"file_path":"a.go"}`, Finished: true},
			}},
		message.Message{ID: "m3", SessionID: sess.ID, Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "call-1", Name: "view", Content: "package main"},
			}},
	)

	req := httptest.NewRequest(http.MethodGet, defaultPath+"/threads/t1/messages", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	r.handleThreadMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Messages []Message `json:"messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Messages) != 3 {
		t.Fatalf("expected 3 AG-UI messages, got %d: %+v", len(resp.Messages), resp.Messages)
	}
	if resp.Messages[0].Role != RoleUser || resp.Messages[0].Content.Text != "hi" {
		t.Fatalf("user message not preserved: %+v", resp.Messages[0])
	}
	if len(resp.Messages[1].ToolCalls) != 1 || resp.Messages[1].ToolCalls[0].ID != "call-1" ||
		resp.Messages[1].ToolCalls[0].Function.Name != "view" {
		t.Fatalf("assistant tool call not preserved: %+v", resp.Messages[1])
	}
	if resp.Messages[2].Role != RoleTool || resp.Messages[2].ToolCallID != "call-1" ||
		resp.Messages[2].Content.Text != "package main" {
		t.Fatalf("tool result pairing lost: %+v", resp.Messages[2])
	}
}

// TestHandleThreadMessagesUnknownThread404s is the PANDO-US-0015 acceptance
// criterion: an unknown thread id 404s on the read route.
func TestHandleThreadMessagesUnknownThread404s(t *testing.T) {
	db := newThreadDB(t)
	r, _, _ := newThreadTestRuntime(t, db)

	req := httptest.NewRequest(http.MethodGet, defaultPath+"/threads/ghost/messages", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "ghost")
	rec := httptest.NewRecorder()
	r.handleThreadMessages(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleDeleteThreadRemovesEverythingAndIsIdempotent is the PANDO-US-0015
// acceptance criterion: DELETE removes messages, session and the agui_threads
// row; a subsequent GET 404s; DELETE is idempotent.
func TestHandleDeleteThreadRemovesEverythingAndIsIdempotent(t *testing.T) {
	db := newThreadDB(t)
	r, sessions, messages := newThreadTestRuntime(t, db)
	ctx := context.Background()

	sess, err := sessions.Create(ctx, "agui: test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	r.threads.put(ctx, "t1", sess.ID, "coder")
	messages.seed(sess.ID, message.Message{
		ID: "m1", SessionID: sess.ID, Role: message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hi"}},
	})
	r.states.get("t1", sess.ID, config.AgentCoder, models.Model{ID: "m1"}, nil)

	deleteReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, defaultPath+"/threads/t1", nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		r.handleDeleteThread(rec, req)
		return rec
	}

	rec := deleteReq()
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", rec.Code, rec.Body.String())
	}

	if _, err := sessions.Get(ctx, sess.ID); err == nil {
		t.Fatal("the session must be deleted")
	}
	if msgs, _ := messages.List(ctx, sess.ID); len(msgs) != 0 {
		t.Fatalf("the session's messages must be deleted, got %v", msgs)
	}
	if _, ok := r.threads.get(ctx, "t1"); ok {
		t.Fatal("the agui_threads binding must be gone")
	}
	if _, ok := r.states.byThread["t1"]; ok {
		t.Fatal("the thread's state document must be gone")
	}

	getReq := httptest.NewRequest(http.MethodGet, defaultPath+"/threads/t1/messages", nil)
	getReq.Header.Set("Authorization", "Bearer secret")
	getReq.SetPathValue("id", "t1")
	getRec := httptest.NewRecorder()
	r.handleThreadMessages(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 after delete", getRec.Code)
	}

	// Idempotent: a repeat DELETE of the same (now unknown) thread still
	// succeeds instead of erroring.
	if rec2 := deleteReq(); rec2.Code != http.StatusNoContent {
		t.Fatalf("repeat DELETE status = %d, want 204", rec2.Code)
	}
}

// TestHandleDeleteThreadUnknownThreadIsIdempotent is the PANDO-US-0015
// acceptance criterion for a thread id this adapter never bound at all.
func TestHandleDeleteThreadUnknownThreadIsIdempotent(t *testing.T) {
	db := newThreadDB(t)
	r, _, _ := newThreadTestRuntime(t, db)

	req := httptest.NewRequest(http.MethodDelete, defaultPath+"/threads/never-existed", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "never-existed")
	rec := httptest.NewRecorder()
	r.handleDeleteThread(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for an unknown thread", rec.Code)
	}
}

// TestDeleteThreadThenNextRunStartsFreshSession is the second half of the
// PANDO-US-0015 acceptance criterion: after DELETE, a subsequent run on the
// same thread id must not be treated as resuming a pre-existing session.
func TestDeleteThreadThenNextRunStartsFreshSession(t *testing.T) {
	db := newThreadDB(t)
	r, sessions, _ := newThreadTestRuntime(t, db)
	ctx := context.Background()

	sess, err := sessions.Create(ctx, "agui: test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	r.threads.put(ctx, "t1", sess.ID, "coder")

	req := httptest.NewRequest(http.MethodDelete, defaultPath+"/threads/t1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "t1")
	r.handleDeleteThread(httptest.NewRecorder(), req)

	newID, existed, err := r.sessionForThread(ctx, "t1", "coder", "hello again", nil)
	if err != nil {
		t.Fatalf("sessionForThread: %v", err)
	}
	if existed {
		t.Fatal("a deleted thread's next run must not be treated as pre-existing")
	}
	if newID == sess.ID {
		t.Fatal("expected a brand new session, not the deleted one")
	}
}
