package code

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

type testEmbedder struct {
	gotNilCtx bool
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

func TestDeleteProject_CascadeDeletesChildren(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
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
		t.Fatalf("create code_symbols: %v", err)
	}

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
