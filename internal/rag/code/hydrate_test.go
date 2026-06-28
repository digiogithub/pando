package code

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/pressly/goose/v3"

	pdb "github.com/digiogithub/pando/internal/db"
)

// setupMigratedDB opens a temp on-disk DB and applies all real goose migrations,
// so the schema matches production (contentless FTS, no source_code column).
func setupMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pando.db")
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		t.Fatalf("wal: %v", err)
	}
	goose.SetBaseFS(pdb.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return conn
}

func TestSourceCodeColumnDropped(t *testing.T) {
	conn := setupMigratedDB(t)
	rows, err := conn.Query("PRAGMA table_info(code_symbols);")
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "source_code" {
			t.Fatalf("code_symbols still has a source_code column after migration")
		}
	}
}

func TestHydrationAndFTSRoundTrip(t *testing.T) {
	conn := setupMigratedDB(t)
	idx := NewCodeIndexer(conn, nil, 1)
	ctx := context.Background()

	root := t.TempDir()
	src := "package sample\n\nfunc Hello() string {\n\treturn \"world\"\n}\n"
	relPath := "sample.go"
	if err := os.WriteFile(filepath.Join(root, relPath), []byte(src), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(src)))

	if _, err := conn.Exec(
		`INSERT INTO code_projects (project_id, name, root_path, indexing_status) VALUES (?,?,?,?)`,
		"proj", "proj", root, "completed"); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Index a single whole-file symbol via the synchronous direct path, which
	// exercises the app-side contentless FTS insert.
	syms := []*CodeSymbol{{
		ID:         "sym-hello",
		ProjectID:  "proj",
		FilePath:   relPath,
		Language:   "go",
		SymbolType: "function",
		Name:       "Hello",
		NamePath:   "Hello",
		StartLine:  1,
		EndLine:    5,
		StartByte:  0,
		EndByte:    len(src),
		SourceCode: src, // fed to FTS only; never stored in the DB
	}}
	symsJSON, _ := json.Marshal(syms)
	if err := idx.IndexFileDirect(ctx, "proj", relPath, "go", hash, symsJSON); err != nil {
		t.Fatalf("index file: %v", err)
	}

	// FTS hybrid search must find it and hydrate the body from disk.
	results, err := idx.HybridSearch(ctx, "proj", "Hello", 10, nil, nil)
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one hybrid result")
	}
	got := results[0].Symbol
	if got.SourceCode != src {
		t.Fatalf("hydrated source mismatch:\n got=%q\nwant=%q", got.SourceCode, src)
	}
	if got.Stale {
		t.Fatalf("symbol marked stale despite matching hash")
	}

	// FindSymbol with body should hydrate too.
	found, err := idx.FindSymbol(ctx, SymbolQuery{ProjectID: "proj", NamePattern: "Hello", Limit: 10, IncludeBody: true})
	if err != nil {
		t.Fatalf("find symbol: %v", err)
	}
	if len(found) == 0 || found[0].SourceCode != src {
		t.Fatalf("find symbol body not hydrated: %+v", found)
	}

	// Edit the file: a hash mismatch must mark the result stale (best-effort).
	edited := src + "\n// changed\n"
	if err := os.WriteFile(filepath.Join(root, relPath), []byte(edited), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	results, err = idx.HybridSearch(ctx, "proj", "Hello", 10, nil, nil)
	if err != nil {
		t.Fatalf("hybrid search 2: %v", err)
	}
	if len(results) == 0 || !results[0].Symbol.Stale {
		t.Fatalf("expected stale=true after file edit, got %+v", results[0].Symbol)
	}
}

func TestReindexDeleteTriggerRemovesFTS(t *testing.T) {
	conn := setupMigratedDB(t)
	idx := NewCodeIndexer(conn, nil, 1)
	ctx := context.Background()

	root := t.TempDir()
	src := "package x\nfunc Alpha() {}\n"
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(src)))
	if _, err := conn.Exec(`INSERT INTO code_projects (project_id, name, root_path, indexing_status) VALUES (?,?,?,?)`,
		"p2", "p2", root, "completed"); err != nil {
		t.Fatalf("project: %v", err)
	}
	syms := []*CodeSymbol{{
		ID: "a", ProjectID: "p2", FilePath: "a.go", Language: "go", SymbolType: "function",
		Name: "Alpha", NamePath: "Alpha", StartLine: 2, EndLine: 2, StartByte: 10, EndByte: len(src), SourceCode: src,
	}}
	symsJSON, _ := json.Marshal(syms)
	if err := idx.IndexFileDirect(ctx, "p2", "a.go", "go", hash, symsJSON); err != nil {
		t.Fatalf("index: %v", err)
	}

	var cnt int
	if err := conn.QueryRow(`SELECT count(*) FROM code_symbols_fts WHERE code_symbols_fts MATCH 'Alpha'`).Scan(&cnt); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 FTS row, got %d", cnt)
	}

	// Reindex the same file with a new hash and no symbols: the delete trigger
	// must purge the stale FTS entry (contentless delete by rowid).
	if err := idx.IndexFileDirect(ctx, "p2", "a.go", "go", hash+"x", json.RawMessage("[]")); err != nil {
		t.Fatalf("reindex empty: %v", err)
	}
	if err := conn.QueryRow(`SELECT count(*) FROM code_symbols_fts WHERE code_symbols_fts MATCH 'Alpha'`).Scan(&cnt); err != nil {
		t.Fatalf("fts count 2: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("expected FTS entry removed after reindex, got %d", cnt)
	}
}
