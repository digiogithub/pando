// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
	ragproxy "github.com/digiogithub/pando/internal/rag/proxy"
)

// openTestKBDB creates an in-memory SQLite DB with the KB schema, mirroring
// production migrations. This is a duplicate of the identically-named helper
// in internal/rag/kb (unexported there, so it cannot be reused across
// packages) kept in sync by hand.
func openTestKBDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// SearchDocumentsWithOptions runs vector and FTS search concurrently on
	// two goroutines; a ":memory:" DSN gives each pooled connection its own
	// separate database unless the pool is capped at one connection.
	db.SetMaxOpenConns(1)

	schema := `
	PRAGMA foreign_keys = ON;

	CREATE TABLE kb_documents (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    file_path  TEXT    NOT NULL UNIQUE,
	    content    TEXT    NOT NULL,
	    metadata   TEXT    NOT NULL DEFAULT '{}',
	    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	    memory_key   TEXT    NOT NULL DEFAULT '',
	    memory_scope TEXT    NOT NULL DEFAULT '',
	    outdated     INTEGER NOT NULL DEFAULT 0,
	    expires_at   DATETIME,
	    hits         INTEGER NOT NULL DEFAULT 0,
	    importance   REAL    NOT NULL DEFAULT 0.5,
	    source       TEXT    NOT NULL DEFAULT ''
	);
	CREATE UNIQUE INDEX idx_kb_documents_memory_key ON kb_documents(memory_key) WHERE memory_key != '';

	CREATE TABLE kb_chunks (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    document_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
	    chunk_index INTEGER NOT NULL DEFAULT 0,
	    content     TEXT    NOT NULL,
	    embedding   BLOB,
	    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	    embedding_model TEXT    NOT NULL DEFAULT '',
	    embedding_dims  INTEGER NOT NULL DEFAULT 0
	);

	CREATE VIRTUAL TABLE kb_fts USING fts5(
	    content,
	    content       = 'kb_chunks',
	    content_rowid = 'id',
	    tokenize      = 'porter unicode61'
	);

	CREATE TABLE kb_links (
	    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
	    source_document_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
	    source_path        TEXT    NOT NULL DEFAULT '',
	    target_slug        TEXT    NOT NULL,
	    target_raw         TEXT    NOT NULL DEFAULT '',
	    label              TEXT    NOT NULL DEFAULT '',
	    position           INTEGER NOT NULL DEFAULT 0,
	    created_at         DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	);
	CREATE INDEX idx_kb_links_source ON kb_links(source_document_id);
	CREATE INDEX idx_kb_links_target ON kb_links(target_slug);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema setup error = %v", err)
	}
	return db
}

// fakeKBEmbedder returns a fixed-size vector per text so store writes work
// without a real embedding provider.
type fakeKBEmbedder struct{}

func (fakeKBEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (fakeKBEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (fakeKBEmbedder) Dimension() int { return 3 }

// blockingKBEmbedder blocks EmbedDocuments until released, so a test can hold
// a reindex mid-flight and observe a second, concurrent request.
type blockingKBEmbedder struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func newBlockingKBEmbedder() *blockingKBEmbedder {
	return &blockingKBEmbedder{
		release: make(chan struct{}),
		started: make(chan struct{}),
	}
}

func (b *blockingKBEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (b *blockingKBEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (b *blockingKBEmbedder) Dimension() int { return 3 }

// newTestKBUpsertServer builds a Server backed by a fresh in-memory KBStore
// with no filesystem mirror configured (sufficient for the document
// upsert/delete tests, which do not touch the mirror on write).
func newTestKBUpsertServer(t *testing.T) (*Server, *kb.KBStore) {
	t.Helper()
	store := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}, token: "test-token"}
	return s, store
}

func doJSONRequest(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, strings.NewReader(body))
}

// ---- POST /api/v1/remembrances/kb/documents ----

func TestHandleUpsertKBDocument_CreatesThenUpdates(t *testing.T) {
	s, store := newTestKBUpsertServer(t)
	ctx := context.Background()

	body := `{"file_path":"docs/example.md","content":"first version","metadata":{"source":"host","n":1}}`
	rec := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	doc, err := store.GetDocument(ctx, "docs/example.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatal("expected document to exist after create")
	}
	if doc.Content != "first version" {
		t.Errorf("content = %q, want %q", doc.Content, "first version")
	}
	if doc.Metadata["source"] != "host" || doc.Metadata["n"] != float64(1) {
		t.Errorf("metadata after create = %#v, want source=host n=1", doc.Metadata)
	}

	// Second call for the same file_path must update, not duplicate, and the
	// metadata sent must come back byte-identical (AC1).
	body2 := `{"file_path":"docs/example.md","content":"second version","metadata":{"source":"host","n":2}}`
	rec2 := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec2, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", body2))
	if rec2.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), `"action":"updated"`) {
		t.Errorf("update response = %s, want action=updated", rec2.Body.String())
	}

	doc2, err := store.GetDocument(ctx, "docs/example.md")
	if err != nil {
		t.Fatalf("GetDocument() (after update) error = %v", err)
	}
	if doc2 == nil {
		t.Fatal("expected document to still exist after update")
	}
	if doc2.Content != "second version" {
		t.Errorf("content after update = %q, want %q", doc2.Content, "second version")
	}
	if doc2.Metadata["source"] != "host" || doc2.Metadata["n"] != float64(2) {
		t.Errorf("metadata after update = %#v, want source=host n=2 (byte-identical round trip)", doc2.Metadata)
	}
}

func TestHandleUpsertKBDocument_OmittedMetadataOnUpdateKeepsStoredMap(t *testing.T) {
	s, store := newTestKBUpsertServer(t)
	ctx := context.Background()

	create := `{"file_path":"docs/keep-meta.md","content":"v1","metadata":{"owner":"alice","score":3}}`
	rec := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", create))
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Update omitting metadata entirely: must not drop what is already stored.
	update := `{"file_path":"docs/keep-meta.md","content":"v2"}`
	rec2 := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec2, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", update))
	if rec2.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec2.Code, rec2.Body.String())
	}

	doc, err := store.GetDocument(ctx, "docs/keep-meta.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc == nil {
		t.Fatal("expected document to exist")
	}
	if doc.Content != "v2" {
		t.Errorf("content = %q, want %q", doc.Content, "v2")
	}
	if doc.Metadata["owner"] != "alice" || doc.Metadata["score"] != float64(3) {
		t.Errorf("metadata after metadata-omitted update = %#v, want the previously stored map preserved", doc.Metadata)
	}
}

func TestHandleUpsertKBDocument_RequiresFilePath(t *testing.T) {
	s, _ := newTestKBUpsertServer(t)
	rec := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", `{"content":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleUpsertKBDocument_RejectsNonPost(t *testing.T) {
	s, _ := newTestKBUpsertServer(t)
	rec := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec, doJSONRequest(http.MethodGet, "/api/v1/remembrances/kb/documents", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleUpsertKBDocument_NilRemembrancesReturns503(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", `{"file_path":"a.md","content":"x"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// ---- DELETE /api/v1/remembrances/kb/documents ----

func TestHandleDeleteKBDocument_RemovesRowAndMirroredFile(t *testing.T) {
	store := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	mirrorDir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(mirrorDir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}}
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/to-delete.md", "content", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	if err := store.WriteDocumentToFilesystem("docs/to-delete.md", "content"); err != nil {
		t.Fatalf("WriteDocumentToFilesystem() error = %v", err)
	}
	mirroredPath := filepath.Join(mirrorDir, "docs", "to-delete.md")
	if _, err := os.Stat(mirroredPath); err != nil {
		t.Fatalf("expected mirrored file to exist before delete: %v", err)
	}

	rec := httptest.NewRecorder()
	s.handleDeleteKBDocument(rec, doJSONRequest(http.MethodDelete, "/api/v1/remembrances/kb/documents", `{"file_path":"docs/to-delete.md"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	doc, err := store.GetDocument(ctx, "docs/to-delete.md")
	if err != nil {
		t.Fatalf("GetDocument() error = %v", err)
	}
	if doc != nil {
		t.Fatal("expected document row to be deleted")
	}
	if _, err := os.Stat(mirroredPath); !os.IsNotExist(err) {
		t.Fatalf("expected mirrored file to be removed, stat err = %v", err)
	}
}

func TestHandleDeleteKBDocument_NilRemembrancesReturns503(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleDeleteKBDocument(rec, doJSONRequest(http.MethodDelete, "/api/v1/remembrances/kb/documents", `{"file_path":"a.md"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleDeleteKBDocument_RejectsNonDelete(t *testing.T) {
	s, _ := newTestKBUpsertServer(t)
	rec := httptest.NewRecorder()
	s.handleDeleteKBDocument(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", `{"file_path":"a.md"}`))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---- POST /api/v1/remembrances/kb/reindex ----

func TestHandleReindexKB_ReturnsStatsAndSkipsUnchanged(t *testing.T) {
	store := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	mirrorDir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(mirrorDir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}}

	if err := os.WriteFile(filepath.Join(mirrorDir, "one.md"), []byte("# One"), 0o644); err != nil {
		t.Fatalf("write one.md: %v", err)
	}

	rec := httptest.NewRecorder()
	s.handleReindexKB(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("first reindex: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"added":1`) {
		t.Fatalf("first reindex body = %s, want added=1", rec.Body.String())
	}

	// A second reindex with nothing changed on disk must not re-embed one.md:
	// mtime is unchanged, so it is skipped (counted as unchanged, not added/updated).
	rec2 := httptest.NewRecorder()
	s.handleReindexKB(rec2, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second reindex: status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), `"added":0`) || !strings.Contains(rec2.Body.String(), `"updated":0`) {
		t.Fatalf("second reindex body = %s, want added=0 updated=0 (mtime unchanged)", rec2.Body.String())
	}

	// Touch the file (new content + advanced mtime) and reindex again: only
	// that one document should be re-embedded (as an update).
	future := time.Now().Add(time.Hour)
	if err := os.WriteFile(filepath.Join(mirrorDir, "one.md"), []byte("# One changed"), 0o644); err != nil {
		t.Fatalf("rewrite one.md: %v", err)
	}
	if err := os.Chtimes(filepath.Join(mirrorDir, "one.md"), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	rec3 := httptest.NewRecorder()
	s.handleReindexKB(rec3, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
	if rec3.Code != http.StatusOK {
		t.Fatalf("third reindex: status = %d, body = %s", rec3.Code, rec3.Body.String())
	}
	if !strings.Contains(rec3.Body.String(), `"updated":1`) {
		t.Fatalf("third reindex body = %s, want updated=1", rec3.Body.String())
	}
}

func TestHandleReindexKB_NoMirrorConfiguredReturns503(t *testing.T) {
	store := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}}
	rec := httptest.NewRecorder()
	s.handleReindexKB(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleReindexKB_NilRemembrancesReturns503(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleReindexKB(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestHandleReindexKB_ConcurrentRunsOneWinsOneConflicts covers AC4: two
// concurrent reindex requests must yield exactly one 200 and one 409, and the
// second must not start a second sync. A blocking embedder holds the first
// sync's single document mid-flight so the second request is guaranteed to
// observe the lock still held.
func TestHandleReindexKB_ConcurrentRunsOneWinsOneConflicts(t *testing.T) {
	embedder := newBlockingKBEmbedder()
	store := kb.NewKBStore(openTestKBDB(t), embedder, 0, 0)
	mirrorDir := t.TempDir()
	if err := store.ConfigureFilesystemMirror(mirrorDir); err != nil {
		t.Fatalf("ConfigureFilesystemMirror() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirrorDir, "slow.md"), []byte("# Slow"), 0o644); err != nil {
		t.Fatalf("write slow.md: %v", err)
	}
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}}

	var codes [2]int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		s.handleReindexKB(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
		codes[0] = rec.Code
	}()

	select {
	case <-embedder.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first reindex to start embedding")
	}

	rec2 := httptest.NewRecorder()
	s.handleReindexKB(rec2, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/reindex", ""))
	codes[1] = rec2.Code

	close(embedder.release)
	wg.Wait()

	got200, got409 := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			got200++
		case http.StatusConflict:
			got409++
		}
	}
	if got200 != 1 || got409 != 1 {
		t.Fatalf("codes = %v, want exactly one 200 and one 409", codes)
	}
	if rec2.Code == http.StatusConflict && !strings.Contains(rec2.Body.String(), "error") {
		t.Errorf("409 body = %s, want a JSON error", rec2.Body.String())
	}
}

// ---- Token auth (AC5): every route answers 401 without a valid token ----

func kbRoutesStack(s *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/remembrances/kb/documents", s.handleUpsertKBDocument)
	mux.HandleFunc("DELETE /api/v1/remembrances/kb/documents", s.handleDeleteKBDocument)
	mux.HandleFunc("POST /api/v1/remembrances/kb/reindex", s.handleReindexKB)
	return s.corsMiddleware(s.basicAuthMiddleware(s.authMiddleware(mux)))
}

func TestKBRoutes_RequireValidToken(t *testing.T) {
	// s.app stays nil: if auth failed to block the request, the handler would
	// nil-deref instead of answering 401, which the assertion below catches.
	s := &Server{token: "the-real-token"}
	stack := kbRoutesStack(s)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"upsert", http.MethodPost, "/api/v1/remembrances/kb/documents", `{"file_path":"a.md","content":"x"}`},
		{"delete", http.MethodDelete, "/api/v1/remembrances/kb/documents", `{"file_path":"a.md"}`},
		{"reindex", http.MethodPost, "/api/v1/remembrances/kb/reindex", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			stack.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s, want 401", rec.Code, rec.Body.String())
			}
		})
	}
}

// ---- Secondary instance proxying (AC6) ----

// TestKBWriteRoutes_SecondaryProxiesToPrimary covers AC6: on a secondary
// instance (KBStore.SetWriteProxy configured), the upsert route must not write
// to the secondary's own local database — the write is forwarded over IPC to
// the primary, exactly as KBStore.AddDocument/UpdateDocument/DeleteDocument
// already do when s.proxy != nil (kb.go). UpdateDocument and DeleteDocument
// share the same s.proxy != nil branch as AddDocument, so this one round trip
// exercises the mechanism common to all three write routes.
func TestKBWriteRoutes_SecondaryProxiesToPrimary(t *testing.T) {
	ctx := context.Background()

	// Primary: real KBStore, reachable over IPC via the RemembrancesWriteDispatcher.
	primaryStore := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	primarySvc := &rag.RemembrancesService{KB: primaryStore}
	dbproxy.RegisterRemembrancesDispatcher(ragproxy.NewRemembrancesWriteDispatcher(primarySvc))
	t.Cleanup(func() { dbproxy.RegisterRemembrancesDispatcher(nil) })

	bus := ipc.NewBus("kb-rest-proxy-test-primary")
	// The db.write JSON-RPC method itself is registered here; dispatchWrite's
	// switch has no case for "KBAddDocument" and falls through to the
	// RemembrancesDispatcher registered above, so a nil db.Querier is fine.
	dbproxy.RegisterHandlers(bus, nil)
	busCtx, busCancel := context.WithCancel(ctx)
	t.Cleanup(busCancel)
	const pubPort, rpcPort = 48930, 48931
	if err := bus.Start(busCtx, pubPort, rpcPort); err != nil {
		t.Fatalf("bus.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = bus.Shutdown() })

	client, err := ipc.NewClient(busCtx)
	if err != nil {
		t.Fatalf("ipc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	proxy := dbproxy.New(nil, client, bus.RPCAddr)

	// Secondary: its own separate local DB — proof that a successful write
	// never lands there directly.
	secondaryDB := openTestKBDB(t)
	secondaryStore := kb.NewKBStore(secondaryDB, fakeKBEmbedder{}, 0, 0)
	secondaryStore.SetWriteProxy(proxy)

	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: secondaryStore}}}

	body := `{"file_path":"proxied/doc.md","content":"hello from secondary","metadata":{"k":"v"}}`
	rec := httptest.NewRecorder()
	s.handleUpsertKBDocument(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/documents", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The primary received it.
	deadline := time.Now().Add(3 * time.Second)
	var doc *kb.Document
	for time.Now().Before(deadline) {
		doc, err = primaryStore.GetDocument(ctx, "proxied/doc.md")
		if err != nil {
			t.Fatalf("primaryStore.GetDocument() error = %v", err)
		}
		if doc != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if doc == nil {
		t.Fatal("expected the primary to have received the proxied write")
	}
	if doc.Content != "hello from secondary" {
		t.Errorf("primary content = %q, want %q", doc.Content, "hello from secondary")
	}

	// The secondary's own local DB must not have written the row directly.
	var count int
	if err := secondaryDB.QueryRow(`SELECT COUNT(*) FROM kb_documents WHERE file_path = ?`, "proxied/doc.md").Scan(&count); err != nil {
		t.Fatalf("secondary count query error = %v", err)
	}
	if count != 0 {
		t.Fatalf("secondary wrote %d rows locally, want 0 (write must go through the proxy, not local db)", count)
	}
}
