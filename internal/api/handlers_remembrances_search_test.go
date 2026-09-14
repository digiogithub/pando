// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/llm/tools"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/code"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// ---- KB search ----

func TestHandleKBSearch_MatchesKBSearchDocumentsToolShape(t *testing.T) {
	store := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}}
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/alpha.md", "Alpha document about hybrid search internals.", map[string]interface{}{"owner": "team-a"}); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}
	if err := store.AddDocument(ctx, "docs/beta.md", "Beta document about unrelated topics.", nil); err != nil {
		t.Fatalf("AddDocument() (beta) error = %v", err)
	}

	// Reference: what the store itself returns for the same query/options —
	// this is exactly what kb_search_documents renders into its resultItem
	// list (internal/llm/tools/remembrances_kb.go), so comparing REST's
	// per-item fields against it proves field-for-field parity.
	want, err := store.SearchDocumentsWithOptions(ctx, "hybrid search internals", 5, kb.SearchOptions{ExcludeOutdated: true})
	if err != nil {
		t.Fatalf("SearchDocumentsWithOptions() error = %v", err)
	}
	if len(want) == 0 {
		t.Fatal("expected at least one search result to compare against")
	}

	rec := httptest.NewRecorder()
	s.handleKBSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/search", `{"query":"hybrid search internals"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got struct {
		Count   int                  `json:"count"`
		Results []kbSearchResultItem `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v (body=%s)", err, rec.Body.String())
	}
	if got.Count != len(want) {
		t.Fatalf("count = %d, want %d", got.Count, len(want))
	}
	for i, r := range want {
		item := got.Results[i]
		if item.FilePath != r.Document.FilePath {
			t.Errorf("result[%d].file_path = %q, want %q", i, item.FilePath, r.Document.FilePath)
		}
		if item.ChunkContent != r.ChunkContent {
			t.Errorf("result[%d].chunk_content = %q, want %q", i, item.ChunkContent, r.ChunkContent)
		}
		if item.Score != r.Score {
			t.Errorf("result[%d].score = %v, want %v", i, item.Score, r.Score)
		}
		if item.Rank != r.Rank {
			t.Errorf("result[%d].rank = %d, want %d", i, item.Rank, r.Rank)
		}
		if !reflect.DeepEqual(item.Metadata, r.Document.Metadata) {
			t.Errorf("result[%d].metadata = %#v, want %#v", i, item.Metadata, r.Document.Metadata)
		}
		if !reflect.DeepEqual(item.Tags, r.Document.Tags) {
			t.Errorf("result[%d].tags = %#v, want %#v", i, item.Tags, r.Document.Tags)
		}
	}
}

func TestHandleKBSearch_DefaultAndMaxLimit(t *testing.T) {
	store := kb.NewKBStore(openTestKBDB(t), fakeKBEmbedder{}, 0, 0)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{KB: store}}}
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		if err := store.AddDocument(ctx, filePathFor(i), "shared query term filler content", nil); err != nil {
			t.Fatalf("AddDocument(%d) error = %v", i, err)
		}
	}

	// No limit given: defaults to 5 (same as kb_search_documents).
	rec := httptest.NewRecorder()
	s.handleKBSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/search", `{"query":"shared query term"}`))
	var got struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Count != 5 {
		t.Fatalf("default limit count = %d, want 5", got.Count)
	}

	// A limit above 20 is clamped to 20 (same ceiling as kb_search_documents).
	rec2 := httptest.NewRecorder()
	s.handleKBSearch(rec2, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/search", `{"query":"shared query term","limit":1000}`))
	var got2 struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &got2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got2.Count != 20 {
		t.Fatalf("clamped limit count = %d, want 20", got2.Count)
	}
}

func filePathFor(i int) string {
	return "docs/bulk-" + time.Now().Add(time.Duration(i)*time.Nanosecond).Format("150405.000000000") + ".md"
}

func TestHandleKBSearch_RequiresQuery(t *testing.T) {
	s, _ := newTestKBUpsertServer(t)
	rec := httptest.NewRecorder()
	s.handleKBSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/search", `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleKBSearch_RejectsNonPost(t *testing.T) {
	s, _ := newTestKBUpsertServer(t)
	rec := httptest.NewRecorder()
	s.handleKBSearch(rec, doJSONRequest(http.MethodGet, "/api/v1/remembrances/kb/search", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleKBSearch_NilRemembrancesReturnsEmptyNotPanic(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleKBSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/kb/search", `{"query":"x"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty result, not panic)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("body = %s, want count:0", rec.Body.String())
	}
}

// ---- Code search ----

// setupCodeSearchTestDB creates an in-memory SQLite DB with the code-index
// schema, mirroring production migrations. Duplicated from the unexported
// setupIndexerTestDB in internal/rag/code (test-only, cannot be reused
// across packages), kept in sync by hand.
func setupCodeSearchTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// HybridSearch runs vector and FTS search concurrently on two goroutines;
	// a ":memory:" DSN gives each pooled connection its own separate database
	// unless the pool is capped at one connection.
	db.SetMaxOpenConns(1)

	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE code_projects (
			project_id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			root_path TEXT NOT NULL,
			language_stats TEXT NOT NULL DEFAULT '{}',
			last_indexed_at DATETIME,
			indexing_status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE code_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
			file_path TEXT NOT NULL,
			language TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			symbols_count INTEGER NOT NULL DEFAULT 0,
			indexed_at DATETIME
		)`,
		`CREATE TABLE code_symbols (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
			file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
			file_path TEXT NOT NULL,
			language TEXT NOT NULL,
			symbol_type TEXT NOT NULL,
			name TEXT NOT NULL,
			name_path TEXT NOT NULL,
			start_line INTEGER NOT NULL DEFAULT 0,
			end_line INTEGER NOT NULL DEFAULT 0,
			start_byte INTEGER NOT NULL DEFAULT 0,
			end_byte INTEGER NOT NULL DEFAULT 0,
			source_code TEXT NOT NULL DEFAULT '',
			signature TEXT NOT NULL DEFAULT '',
			doc_string TEXT NOT NULL DEFAULT '',
			parent_id TEXT,
			metadata TEXT NOT NULL DEFAULT '{}',
			embedding BLOB,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE VIRTUAL TABLE code_symbols_fts USING fts5(
			name,
			name_path,
			doc_string,
			source_code,
			content = 'code_symbols',
			content_rowid = 'rowid',
			tokenize = 'porter unicode61'
		)`,
		`CREATE TABLE code_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
			file_id INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
			edge_type TEXT NOT NULL,
			src_file TEXT NOT NULL DEFAULT '',
			src_symbol TEXT NOT NULL DEFAULT '',
			dst_name TEXT NOT NULL DEFAULT '',
			dst_path TEXT NOT NULL DEFAULT '',
			start_line INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema setup error: %v (stmt=%s)", err, stmt)
		}
	}
	return db
}

func insertCodeSearchProject(t *testing.T, db *sql.DB, projectID, rootPath string, now time.Time) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO code_projects (project_id, name, root_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, projectID, projectID, rootPath, now, now); err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}
}

func insertCodeSearchFile(t *testing.T, db *sql.DB, projectID, filePath, language string, now time.Time) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count, indexed_at) VALUES (?, ?, ?, ?, 0, ?)`, projectID, filePath, language, "hash-"+filePath, now)
	if err != nil {
		t.Fatalf("insert file %s: %v", filePath, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("file last insert id %s: %v", filePath, err)
	}
	return id
}

func insertCodeSearchSymbol(t *testing.T, db *sql.DB, fileID int64, projectID, filePath, id, namePath, symbolType, name, sourceCode, signature, docString string, now time.Time) {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO code_symbols (
			id, project_id, file_id, file_path, language, symbol_type, name, name_path,
			start_line, end_line, start_byte, end_byte, source_code, signature, doc_string,
			parent_id, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'go', ?, ?, ?, 1, 10, 1, 100, ?, ?, ?, NULL, '{}', ?, ?)`,
		id, projectID, fileID, filePath, symbolType, name, namePath, sourceCode, signature, docString, now, now)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", id, err)
	}
	rowID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("symbol last insert id %s: %v", id, err)
	}
	if _, err := db.Exec(`INSERT INTO code_symbols_fts(rowid, name, name_path, doc_string, source_code) VALUES (?, ?, ?, ?, ?)`, rowID, name, namePath, docString, sourceCode); err != nil {
		t.Fatalf("insert symbol fts %s: %v", id, err)
	}
}

type fixedVectorTestEmbedder struct{ query []float32 }

func (e fixedVectorTestEmbedder) Dimension() int { return len(e.query) }
func (e fixedVectorTestEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return append([]float32(nil), e.query...), nil
}
func (e fixedVectorTestEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

// newCodeSearchFixture builds a real CodeIndexer with a handful of symbols
// indexed directly (no tree-sitter parsing needed): two Go symbols matching
// the test query at different relevance, and one markdown "doc" symbol that
// must be excluded from results unless include_docs is set.
func newCodeSearchFixture(t *testing.T) (*code.CodeIndexer, string) {
	t.Helper()
	db := setupCodeSearchTestDB(t)
	now := time.Now().UTC()
	const projectID = "proj1"
	insertCodeSearchProject(t, db, projectID, "/tmp/proj1", now)

	goFileID := insertCodeSearchFile(t, db, projectID, "internal/rag/code/indexer.go", "go", now)
	insertCodeSearchSymbol(t, db, goFileID, projectID, "internal/rag/code/indexer.go",
		"sym-relevant", "/CodeTools/Indexer", "function", "Indexer",
		"func Indexer() { hybrid search relevance ranking }", "func Indexer()", "Relevant hybrid search target.", now)
	insertCodeSearchSymbol(t, db, goFileID, projectID, "internal/rag/code/indexer.go",
		"sym-other", "/CodeTools/Helper", "function", "Helper",
		"func Helper() { unrelated helper }", "func Helper()", "Unrelated helper.", now)

	mdFileID := insertCodeSearchFile(t, db, projectID, "docs/GUIDE.md", "markdown", now)
	insertCodeSearchSymbol(t, db, mdFileID, projectID, "docs/GUIDE.md",
		"sym-doc", "/Guides/HybridSearch", "namespace", "Hybrid Search Guide",
		"hybrid search relevance ranking guide", "", "Doc guide about hybrid search relevance ranking.", now)

	idx := code.NewCodeIndexer(db, fixedVectorTestEmbedder{query: []float32{1, 0}}, 1)
	return idx, projectID
}

// TestHandleCodeSearch_MatchesCodeHybridSearchToolOrdering covers AC2/AC3:
// the REST route must rank identically to code_hybrid_search (same shared
// tools.RankAndFilterHybrid helper) for the same arguments, including
// applying include_docs and min_score.
func TestHandleCodeSearch_MatchesCodeHybridSearchToolOrdering(t *testing.T) {
	idx, projectID := newCodeSearchFixture(t)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{Code: idx}}}
	toolCall := tools.NewCodeHybridSearchTool(idx)

	query := "hybrid search relevance ranking"
	body := `{"project_id":"` + projectID + `","query":"` + query + `","limit":10}`

	// Reference: the real MCP tool, same arguments.
	toolResp, err := toolCall.Run(context.Background(), tools.ToolCall{
		Name:  "code_hybrid_search",
		Input: body,
	})
	if err != nil {
		t.Fatalf("tool.Run() error = %v", err)
	}
	if toolResp.IsError {
		t.Fatalf("tool.Run() returned error response: %s", toolResp.Content)
	}
	var toolMeta struct {
		Results []tools.HybridResultItem `json:"results"`
	}
	if err := json.Unmarshal([]byte(toolResp.Metadata), &toolMeta); err != nil {
		t.Fatalf("unmarshal tool metadata: %v (metadata=%s)", err, toolResp.Metadata)
	}
	if len(toolMeta.Results) == 0 {
		t.Fatal("expected the tool to return at least one result")
	}

	// REST, same arguments.
	rec := httptest.NewRecorder()
	s.handleCodeSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/code/search", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var restResp struct {
		Results []tools.HybridResultItem `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &restResp); err != nil {
		t.Fatalf("unmarshal REST response: %v (body=%s)", err, rec.Body.String())
	}

	if !reflect.DeepEqual(restResp.Results, toolMeta.Results) {
		t.Fatalf("REST results = %#v,\nwant (tool) = %#v", restResp.Results, toolMeta.Results)
	}
	// The markdown doc symbol must be excluded by default in both.
	for _, r := range restResp.Results {
		if r.Kind == "doc" {
			t.Errorf("doc symbol %q present in REST results with include_docs unset", r.FilePath)
		}
	}
}

func TestHandleCodeSearch_IncludeDocsAndMinScore(t *testing.T) {
	idx, projectID := newCodeSearchFixture(t)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{Code: idx}}}

	rec := httptest.NewRecorder()
	body := `{"project_id":"` + projectID + `","query":"hybrid search relevance ranking","limit":10,"include_docs":true}`
	s.handleCodeSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/code/search", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []tools.HybridResultItem `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	foundDoc := false
	for _, r := range resp.Results {
		if r.Kind == "doc" {
			foundDoc = true
		}
	}
	if !foundDoc {
		t.Fatal("expected the doc symbol to be present with include_docs=true")
	}
}

func TestHandleCodeSearch_RequiresProjectIDAndQuery(t *testing.T) {
	idx, _ := newCodeSearchFixture(t)
	s := &Server{app: &app.App{Remembrances: &rag.RemembrancesService{Code: idx}}}

	rec := httptest.NewRecorder()
	s.handleCodeSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/code/search", `{"query":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing project_id: status = %d, want 400", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	s.handleCodeSearch(rec2, doJSONRequest(http.MethodPost, "/api/v1/remembrances/code/search", `{"project_id":"p"}`))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("missing query: status = %d, want 400", rec2.Code)
	}
}

func TestHandleCodeSearch_RejectsNonPost(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleCodeSearch(rec, doJSONRequest(http.MethodGet, "/api/v1/remembrances/code/search", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleCodeSearch_NilRemembrancesReturnsEmptyNotPanic(t *testing.T) {
	s := &Server{app: &app.App{}}
	rec := httptest.NewRecorder()
	s.handleCodeSearch(rec, doJSONRequest(http.MethodPost, "/api/v1/remembrances/code/search", `{"project_id":"p","query":"x"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty result, not panic)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"count":0`) {
		t.Fatalf("body = %s, want count:0", rec.Body.String())
	}
}

// ---- Token auth (AC4): 401 without a valid token, handler never entered ----

func searchRoutesStack(s *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/remembrances/kb/search", s.handleKBSearch)
	mux.HandleFunc("POST /api/v1/remembrances/code/search", s.handleCodeSearch)
	return s.corsMiddleware(s.basicAuthMiddleware(s.authMiddleware(mux)))
}

func TestSearchRoutes_RequireValidToken(t *testing.T) {
	// s.app stays nil: if auth failed to block the request before the mux
	// even dispatched to the handler, the handler would nil-deref instead of
	// answering 401.
	s := &Server{token: "the-real-token"}
	stack := searchRoutesStack(s)

	cases := []struct {
		name, path, body string
	}{
		{"kb", "/api/v1/remembrances/kb/search", `{"query":"x"}`},
		{"code", "/api/v1/remembrances/code/search", `{"project_id":"p","query":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			stack.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s, want 401", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSearchRoutes_RejectNonPostThroughMux(t *testing.T) {
	s := &Server{token: "the-real-token"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/remembrances/kb/search", s.handleKBSearch)
	mux.HandleFunc("POST /api/v1/remembrances/code/search", s.handleCodeSearch)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/remembrances/kb/search", nil)
	req.Header.Set("X-Pando-Token", "the-real-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on kb/search via mux: status = %d, want 405", rec.Code)
	}
}
