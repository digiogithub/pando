package tools

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/rag/kb"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// openMarkOutdatedTestStore builds an in-memory KB store with just enough schema
// for the mark-outdated path (document row + the outdated flag). No embedder is
// configured, so documents are inserted with a direct SQL statement rather than
// AddDocument (which would embed non-empty content).
func openMarkOutdatedTestStore(t *testing.T) (*kb.KBStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

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

	CREATE TABLE kb_chunks (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    document_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
	    chunk_index INTEGER NOT NULL DEFAULT 0,
	    content     TEXT    NOT NULL,
	    embedding   BLOB,
	    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
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
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema setup error = %v", err)
	}
	return kb.NewKBStore(db, nil, 0, 0), db
}

func insertMarkOutdatedDoc(t *testing.T, db *sql.DB, filePath, content string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.Exec(
		`INSERT INTO kb_documents (file_path, content, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		filePath, content, now, now,
	); err != nil {
		t.Fatalf("insert document error = %v", err)
	}
}

func outdatedFlag(t *testing.T, db *sql.DB, filePath string) int {
	t.Helper()
	var outdated int
	if err := db.QueryRow(`SELECT outdated FROM kb_documents WHERE file_path = ?`, filePath).Scan(&outdated); err != nil {
		t.Fatalf("read outdated flag error = %v", err)
	}
	return outdated
}

func TestKBMarkOutdatedRequiresFilePath(t *testing.T) {
	store, _ := openMarkOutdatedTestStore(t)
	tool := NewKBMarkOutdatedTool(store)

	resp, err := tool.Run(context.Background(), ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected an error response when file_path is missing")
	}
	if !strings.Contains(resp.Content, "file_path is required") {
		t.Errorf("content = %q, want it to mention the missing file_path", resp.Content)
	}
}

func TestKBMarkOutdatedReportsMissingDocument(t *testing.T) {
	store, _ := openMarkOutdatedTestStore(t)
	tool := NewKBMarkOutdatedTool(store)

	resp, err := tool.Run(context.Background(), ToolCall{Input: `{"file_path":"pando/plans/ghost.md"}`})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected a plain response for a missing doc, got error: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "Document not found") {
		t.Errorf("content = %q, want it to report the document as not found", resp.Content)
	}
}

func TestKBMarkOutdatedFlagsExistingDocument(t *testing.T) {
	store, db := openMarkOutdatedTestStore(t)
	insertMarkOutdatedDoc(t, db, "pando/plans/old.md", "superseded plan")
	tool := NewKBMarkOutdatedTool(store)

	resp, err := tool.Run(context.Background(), ToolCall{Input: `{"file_path":"pando/plans/old.md"}`})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected success, got error: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "marked outdated") {
		t.Errorf("content = %q, want a confirmation", resp.Content)
	}
	if got := outdatedFlag(t, db, "pando/plans/old.md"); got != 1 {
		t.Errorf("outdated flag = %d, want 1", got)
	}
}

func TestKBMarkOutdatedIsIdempotent(t *testing.T) {
	store, db := openMarkOutdatedTestStore(t)
	insertMarkOutdatedDoc(t, db, "pando/plans/old.md", "superseded plan")
	tool := NewKBMarkOutdatedTool(store)

	for range 2 {
		resp, err := tool.Run(context.Background(), ToolCall{Input: `{"file_path":"pando/plans/old.md"}`})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if resp.IsError {
			t.Fatalf("expected success, got error: %q", resp.Content)
		}
	}
	if got := outdatedFlag(t, db, "pando/plans/old.md"); got != 1 {
		t.Errorf("outdated flag after re-mark = %d, want 1", got)
	}
}
