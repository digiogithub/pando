package code

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

type testEmbedder struct {
	gotNilCtx bool
}

func setupIndexerTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		t.Fatalf("enable foreign keys: %v", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE code_projects (
			project_id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			root_path TEXT NOT NULL,
			language_stats TEXT NOT NULL DEFAULT '{}',
			last_indexed_at DATETIME,
			indexing_status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME,
			updated_at DATETIME
		);
	`); err != nil {
		db.Close()
		t.Fatalf("create code_projects: %v", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE code_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
			file_path TEXT NOT NULL,
			language TEXT NOT NULL,
			file_hash TEXT NOT NULL,
			symbols_count INTEGER NOT NULL DEFAULT 0,
			indexed_at DATETIME
		);
	`); err != nil {
		db.Close()
		t.Fatalf("create code_files: %v", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE code_symbols (
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
		);
	`); err != nil {
		db.Close()
		t.Fatalf("create code_symbols: %v", err)
	}

	if _, err := db.Exec(`
		CREATE VIRTUAL TABLE code_symbols_fts USING fts5(
			name,
			name_path,
			doc_string,
			source_code,
			content = 'code_symbols',
			content_rowid = 'rowid',
			tokenize = 'porter unicode61'
		);
	`); err != nil {
		db.Close()
		t.Fatalf("create code_symbols_fts: %v", err)
	}

	return db
}

func insertTestCodeProject(t *testing.T, db *sql.DB, projectID, rootPath string, now time.Time) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO code_projects (project_id, name, root_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, projectID, projectID, rootPath, now, now); err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}
}

func insertTestCodeFile(t *testing.T, db *sql.DB, projectID, filePath string, now time.Time) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count, indexed_at) VALUES (?, ?, ?, ?, ?, ?)`, projectID, filePath, "go", "hash-"+filePath, 0, now)
	if err != nil {
		t.Fatalf("insert file %s: %v", filePath, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("file last insert id %s: %v", filePath, err)
	}
	return id
}

func insertTestCodeSymbol(t *testing.T, db *sql.DB, fileID int64, projectID, filePath, id, namePath, symbolType, name, sourceCode, signature, docString string, metadata map[string]any, now time.Time) int64 {
	t.Helper()
	metaJSON := "{}"
	if len(metadata) > 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			t.Fatalf("marshal metadata for %s: %v", id, err)
		}
		metaJSON = string(b)
	}
	res, err := db.Exec(`
		INSERT INTO code_symbols (
			id, project_id, file_id, file_path, language, symbol_type, name, name_path,
			start_line, end_line, start_byte, end_byte, source_code, signature, doc_string,
			parent_id, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'go', ?, ?, ?, 1, 10, 1, 100, ?, ?, ?, NULL, ?, ?, ?)`,
		id, projectID, fileID, filePath, symbolType, name, namePath, sourceCode, signature, docString, metaJSON, now, now)
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
	return rowID
}

func findSymbolByID(symbols []*CodeSymbol, id string) *CodeSymbol {
	for _, sym := range symbols {
		if sym != nil && sym.ID == id {
			return sym
		}
	}
	return nil
}

type fixedVectorEmbedder struct {
	query []float32
}

func (e fixedVectorEmbedder) Dimension() int { return len(e.query) }

func (e fixedVectorEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return append([]float32(nil), e.query...), nil
}

func (e fixedVectorEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (e *testEmbedder) Dimension() int {
	return 2
}

func (e *testEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if ctx == nil {
		e.gotNilCtx = true
	}
	return []float32{0.1}, nil
}

func (e *testEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	if ctx == nil {
		e.gotNilCtx = true
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func TestEmbedSymbols_NilContextUsesBackground(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE code_symbols (id TEXT PRIMARY KEY, embedding BLOB)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO code_symbols (id) VALUES ('sym-1')`); err != nil {
		t.Fatalf("seed symbol: %v", err)
	}

	emb := &testEmbedder{}
	idx := &CodeIndexer{db: db, embedder: emb}

	syms := []*treesitter.CodeSymbol{
		{
			ID:         "sym-1",
			NamePath:   "pkg.Func",
			SourceCode: "func Func() {}",
		},
	}

	if err := idx.embedSymbols(nil, "proj", 1, syms); err != nil {
		t.Fatalf("embedSymbols returned error: %v", err)
	}
	if emb.gotNilCtx {
		t.Fatalf("embedder received nil context")
	}
}

func TestHasProjectAndDeleteFile(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO code_projects (project_id, name, root_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "proj1", "proj1", "/tmp/proj1", now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO code_projects (project_id, name, root_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, "proj2", "proj2", "/tmp/proj2", now, now); err != nil {
		t.Fatalf("insert project2: %v", err)
	}

	idx := NewCodeIndexer(db, nil, 1)
	ok, err := idx.HasProject(context.Background(), "proj1", "/tmp/proj1")
	if err != nil {
		t.Fatalf("HasProject returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected HasProject to return true for matching root")
	}

	ok, err = idx.HasProject(context.Background(), "proj1", "/tmp/other")
	if err != nil {
		t.Fatalf("HasProject mismatch returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected HasProject to return false for mismatched root")
	}

	ok, err = idx.HasProject(context.Background(), "missing", "/tmp/proj1")
	if err != nil {
		t.Fatalf("HasProject missing returned error: %v", err)
	}
	if ok {
		t.Fatalf("expected HasProject to return false for missing project")
	}

	res, err := db.Exec(`INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count, indexed_at) VALUES ('proj1', 'a.go', 'go', 'h1', 1, ?)`, now)
	if err != nil {
		t.Fatalf("insert file proj1: %v", err)
	}
	fileID1, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id proj1: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO code_symbols (id, project_id, file_id, file_path, language, symbol_type, name, name_path, created_at, updated_at)
		VALUES ('sym1', 'proj1', ?, 'a.go', 'go', 'function', 'A', '/A', ?, ?)`, fileID1, now, now); err != nil {
		t.Fatalf("insert symbol proj1: %v", err)
	}

	if err := idx.DeleteFile(context.Background(), "proj1", "a.go"); err != nil {
		t.Fatalf("DeleteFile returned error: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM code_files WHERE project_id = 'proj1' AND file_path = 'a.go'`).Scan(&count); err != nil {
		t.Fatalf("count proj1 file: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected proj1 file to be deleted, got %d rows", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM code_symbols WHERE project_id = 'proj1'`).Scan(&count); err != nil {
		t.Fatalf("count proj1 symbols: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected proj1 symbols to be cascade-deleted, got %d rows", count)
	}

	if err := idx.DeleteFile(context.Background(), "proj1", "missing.go"); !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist for missing file, got %v", err)
	}
}

func TestIndexProjectSync_IgnoresPermissionWalkErrors(t *testing.T) {
	idx := NewCodeIndexer(nil, nil, 1)
	job := &IndexingJob{ProjectPath: "/tmp/project"}
	permErr := &fs.PathError{Op: "open", Path: "/tmp/project/secret", Err: fs.ErrPermission}

	if !shouldIgnoreCodePathError(permErr) {
		t.Fatalf("expected permission error to be ignored")
	}

	idx.appendJobWarning(job, pathWarning("secret", errors.New("walk skipped: permission denied")))
	if len(job.Warnings) != 1 {
		t.Fatalf("expected warning to be recorded, got %d", len(job.Warnings))
	}
}

func TestReindexFile_IgnoresPermissionDenied(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	root := t.TempDir()
	filePath := filepath.Join(root, "restricted.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	now := time.Now().UTC()
	insertTestCodeProject(t, db, "proj-perm", root, now)

	idx := NewCodeIndexer(db, nil, 1)
	originalReadFile := osReadFileForTest
	osReadFileForTest = func(name string) ([]byte, error) {
		if name == filePath {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		return os.ReadFile(name)
	}
	defer func() { osReadFileForTest = originalReadFile }()

	if err := idx.ReindexFile(context.Background(), "proj-perm", "restricted.go"); err != nil {
		t.Fatalf("expected permission denied to be ignored, got %v", err)
	}
}

func TestDeleteProject_CascadeDeletesChildren(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO code_projects (project_id, name, root_path) VALUES ('proj1', 'proj1', '/tmp/proj1')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO code_projects (project_id, name, root_path) VALUES ('proj2', 'proj2', '/tmp/proj2')`); err != nil {
		t.Fatalf("insert project2: %v", err)
	}

	res, err := db.Exec(`INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count) VALUES ('proj1', 'a.go', 'go', 'h1', 1)`)
	if err != nil {
		t.Fatalf("insert file proj1: %v", err)
	}
	fileID1, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id proj1: %v", err)
	}

	res, err = db.Exec(`INSERT INTO code_files (project_id, file_path, language, file_hash, symbols_count) VALUES ('proj2', 'b.go', 'go', 'h2', 1)`)
	if err != nil {
		t.Fatalf("insert file proj2: %v", err)
	}
	fileID2, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id proj2: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO code_symbols (id, project_id, file_id, file_path, language, symbol_type, name, name_path)
		VALUES ('sym1', 'proj1', ?, 'a.go', 'go', 'function', 'A', '/A')`, fileID1); err != nil {
		t.Fatalf("insert symbol proj1: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO code_symbols (id, project_id, file_id, file_path, language, symbol_type, name, name_path)
		VALUES ('sym2', 'proj2', ?, 'b.go', 'go', 'function', 'B', '/B')`, fileID2); err != nil {
		t.Fatalf("insert symbol proj2: %v", err)
	}

	idx := NewCodeIndexer(db, nil, 1)
	if err := idx.DeleteProject(context.Background(), "proj1"); err != nil {
		t.Fatalf("DeleteProject returned error: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM code_projects WHERE project_id = 'proj1'`).Scan(&count); err != nil {
		t.Fatalf("count proj1 in code_projects: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected proj1 to be deleted from code_projects, got %d rows", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM code_files WHERE project_id = 'proj1'`).Scan(&count); err != nil {
		t.Fatalf("count proj1 in code_files: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected proj1 files to be cascade-deleted, got %d rows", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM code_symbols WHERE project_id = 'proj1'`).Scan(&count); err != nil {
		t.Fatalf("count proj1 in code_symbols: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected proj1 symbols to be cascade-deleted, got %d rows", count)
	}

	// Ensure other projects are untouched.
	if err := db.QueryRow(`SELECT COUNT(*) FROM code_projects WHERE project_id = 'proj2'`).Scan(&count); err != nil {
		t.Fatalf("count proj2 in code_projects: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected proj2 to remain in code_projects, got %d rows", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM code_files WHERE project_id = 'proj2'`).Scan(&count); err != nil {
		t.Fatalf("count proj2 in code_files: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected proj2 file to remain, got %d rows", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM code_symbols WHERE project_id = 'proj2'`).Scan(&count); err != nil {
		t.Fatalf("count proj2 in code_symbols: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected proj2 symbol to remain, got %d rows", count)
	}
}

func TestFindSymbolHydratesPersistedFields(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 25, 10, 11, 12, 0, time.UTC)
	insertTestCodeProject(t, db, "proj1", "/tmp/proj1", now)
	fileID := insertTestCodeFile(t, db, "proj1", "internal/rag/code/indexer.go", now)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-1", "/CodeTools/FindSymbol", "function", "FindSymbol",
		"func FindSymbol(ctx context.Context) {}", "func FindSymbol(ctx context.Context)",
		"Returns persisted timestamps.", map[string]any{"kind": "test"},
		now,
	)

	idx := NewCodeIndexer(db, nil, 1)
	symbols, err := idx.FindSymbol(context.Background(), SymbolQuery{
		ProjectID:       "proj1",
		NamePathPattern: "/CodeTools/FindSymbol",
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("FindSymbol returned error: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}

	sym := symbols[0]
	if sym.CreatedAt.IsZero() || sym.UpdatedAt.IsZero() {
		t.Fatalf("expected persisted timestamps, got created_at=%v updated_at=%v", sym.CreatedAt, sym.UpdatedAt)
	}
	if !sym.CreatedAt.Equal(now) || !sym.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected timestamps: got %v and %v", sym.CreatedAt, sym.UpdatedAt)
	}
	if sym.StartByte != 1 || sym.EndByte != 100 {
		t.Fatalf("unexpected byte offsets: %+v", sym)
	}
	if sym.Signature != "func FindSymbol(ctx context.Context)" {
		t.Fatalf("unexpected signature: %q", sym.Signature)
	}
	if sym.Metadata["kind"] != "test" {
		t.Fatalf("unexpected metadata: %+v", sym.Metadata)
	}
}

func TestGetSymbolsOverviewHydratesPersistedFields(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 25, 10, 11, 12, 0, time.UTC)
	insertTestCodeProject(t, db, "proj1", "/tmp/proj1", now)
	fileID := insertTestCodeFile(t, db, "proj1", "internal/rag/code/indexer.go", now)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-2", "/CodeTools/GetSymbolsOverview", "function", "GetSymbolsOverview",
		"func GetSymbolsOverview() {}", "func GetSymbolsOverview()",
		"Returns an overview.", map[string]any{"scope": "overview"},
		now,
	)

	idx := NewCodeIndexer(db, nil, 1)
	symbols, err := idx.GetSymbolsOverview(context.Background(), "proj1", "internal/rag/code/indexer.go", 10)
	if err != nil {
		t.Fatalf("GetSymbolsOverview returned error: %v", err)
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}

	sym := symbols[0]
	if sym.CreatedAt.IsZero() || sym.UpdatedAt.IsZero() {
		t.Fatalf("expected persisted timestamps, got created_at=%v updated_at=%v", sym.CreatedAt, sym.UpdatedAt)
	}
	if sym.Metadata["scope"] != "overview" {
		t.Fatalf("unexpected metadata: %+v", sym.Metadata)
	}
}

func TestHybridSearchPrefersLexicalMatches(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 25, 10, 11, 12, 0, time.UTC)
	insertTestCodeProject(t, db, "proj1", "/tmp/proj1", now)
	fileID := insertTestCodeFile(t, db, "proj1", "internal/rag/code/indexer.go", now)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-relevant", "/CodeTools/Indexer", "function", "Indexer",
		"func Indexer() { created_at updated_at code_find_symbol code_hybrid_search }",
		"func Indexer()", "Relevant hybrid search target.",
		nil, now,
	)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-unrelated", "/Editor/Palette", "function", "Palette",
		"func Palette() {}", "func Palette()", "Unrelated UI code.",
		nil, now,
	)

	idx := NewCodeIndexer(db, fixedVectorEmbedder{query: []float32{1, 0}}, 1)
	// Force the unrelated symbol to look slightly better to vector search.
	if _, err := db.Exec(`UPDATE code_symbols SET embedding = ? WHERE id = ?`, serializeFloat32([]float32{1, 0}), "sym-unrelated"); err != nil {
		t.Fatalf("seed unrelated embedding: %v", err)
	}
	if _, err := db.Exec(`UPDATE code_symbols SET embedding = ? WHERE id = ?`, serializeFloat32([]float32{0.8, 0.2}), "sym-relevant"); err != nil {
		t.Fatalf("seed relevant embedding: %v", err)
	}

	results, err := idx.HybridSearch(context.Background(), "proj1", "code_hybrid_search code_find_symbol created_at updated_at", 5, nil, nil)
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid search results")
	}
	if results[0].Symbol.ID != "sym-relevant" {
		t.Fatalf("expected relevant symbol first, got %s (all results: %+v)", results[0].Symbol.ID, results)
	}
}

func TestHybridSearchPrefersCodeSymbolsOverMarkdownNoise(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 27, 11, 12, 13, 0, time.UTC)
	insertTestCodeProject(t, db, "proj1", "/tmp/proj1", now)

	mdFileID := insertTestCodeFile(t, db, "proj1", "AGENTS.md", now)
	insertTestCodeSymbol(
		t, db, mdFileID, "proj1", "AGENTS.md",
		"sym-doc", "/Guides/HybridSearch", "namespace", "Hybrid Search Guide",
		"hybrid search ranking guidance for the codebase",
		"Hybrid Search Guide", "General hybrid search documentation.",
		nil, now,
	)

	goFileID := insertTestCodeFile(t, db, "proj1", "internal/rag/code/indexer.go", now)
	insertTestCodeSymbol(
		t, db, goFileID, "proj1", "internal/rag/code/indexer.go",
		"sym-code", "/CodeIndexer/HybridSearch", "method", "HybridSearch",
		"func (c *CodeIndexer) HybridSearch() { code_hybrid_search ranking lexical boost }",
		"func (c *CodeIndexer) HybridSearch()", "Ranks code_hybrid_search results with lexical boost.",
		nil, now,
	)

	idx := NewCodeIndexer(db, fixedVectorEmbedder{query: []float32{1, 0}}, 1)
	if _, err := db.Exec(`UPDATE code_symbols SET embedding = ? WHERE id = ?`, serializeFloat32([]float32{1, 0}), "sym-doc"); err != nil {
		t.Fatalf("seed markdown embedding: %v", err)
	}
	if _, err := db.Exec(`UPDATE code_symbols SET embedding = ? WHERE id = ?`, serializeFloat32([]float32{0.92, 0.08}), "sym-code"); err != nil {
		t.Fatalf("seed code embedding: %v", err)
	}

	results, err := idx.HybridSearch(context.Background(), "proj1", "code_hybrid_search ranking lexical boost", 5, nil, nil)
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid search results")
	}
	if results[0].Symbol.ID != "sym-code" {
		t.Fatalf("expected code symbol first, got %s (all results: %+v)", results[0].Symbol.ID, results)
	}
}

func TestHybridSearchBoostsSourceMatchesWhenStructureIsSparse(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 27, 11, 12, 13, 0, time.UTC)
	insertTestCodeProject(t, db, "proj1", "/tmp/proj1", now)
	fileID := insertTestCodeFile(t, db, "proj1", "internal/rag/code/indexer.go", now)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-source", "/CodeIndexer/Search", "function", "Search",
		"func Search() { lexicalBoost buildCodeSearchTerms buildCodeFTSQuery boostHybridResults }",
		"func Search()", "Search helper.",
		nil, now,
	)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/other.go",
		"sym-shallow", "/Docs/Boost", "function", "Boost",
		"func Boost() {}",
		"func Boost()", "Mentions boost once.",
		nil, now,
	)

	idx := NewCodeIndexer(db, fixedVectorEmbedder{query: []float32{1, 0}}, 1)
	if _, err := db.Exec(`UPDATE code_symbols SET embedding = ? WHERE id = ?`, serializeFloat32([]float32{0.95, 0.05}), "sym-shallow"); err != nil {
		t.Fatalf("seed shallow embedding: %v", err)
	}
	if _, err := db.Exec(`UPDATE code_symbols SET embedding = ? WHERE id = ?`, serializeFloat32([]float32{0.82, 0.18}), "sym-source"); err != nil {
		t.Fatalf("seed source embedding: %v", err)
	}

	results, err := idx.HybridSearch(context.Background(), "proj1", "lexicalBoost buildCodeSearchTerms boostHybridResults", 5, nil, nil)
	if err != nil {
		t.Fatalf("HybridSearch returned error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid search results")
	}
	if results[0].Symbol.ID != "sym-source" {
		t.Fatalf("expected source-heavy symbol first, got %s (all results: %+v)", results[0].Symbol.ID, results)
	}
}

func TestSearchPatternMatchesNamePathAndRegex(t *testing.T) {
	db := setupIndexerTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 25, 10, 11, 12, 0, time.UTC)
	insertTestCodeProject(t, db, "proj1", "/tmp/proj1", now)
	fileID := insertTestCodeFile(t, db, "proj1", "internal/rag/code/indexer.go", now)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-settings", "/Config/Settings", "function", "Settings",
		"func Settings() {}", "func Settings()", "Configuration symbol.",
		nil, now,
	)
	insertTestCodeSymbol(
		t, db, fileID, "proj1", "internal/rag/code/indexer.go",
		"sym-other", "/Other/Thing", "function", "Thing",
		"func Thing() { target := \"x\" }", "func Thing()", "Different symbol.",
		nil, now,
	)

	idx := NewCodeIndexer(db, nil, 1)
	literal, err := idx.SearchPattern(context.Background(), "proj1", "Config/Settings", false, false, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchPattern literal returned error: %v", err)
	}
	if len(literal) != 1 || literal[0].ID != "sym-settings" {
		t.Fatalf("expected literal match on name_path, got %+v", literal)
	}

	regex, err := idx.SearchPattern(context.Background(), "proj1", `Config/Settings`, true, true, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchPattern regex returned error: %v", err)
	}
	if len(regex) != 1 || regex[0].ID != "sym-settings" {
		t.Fatalf("expected regex match on name_path, got %+v", regex)
	}
}
