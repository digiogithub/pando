package kb

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// openTestKBDB creates an in-memory SQLite DB with the KB schema, mirroring the
// production migrations (20260311000001_add_kb.sql + 20260611000001_add_kb_memory.sql
// + 20260713000001_add_kb_links.sql). Foreign keys are enabled as in production
// (internal/db/connect.go) so ON DELETE CASCADE behaves the same.
func openTestKBDB(t *testing.T) *sql.DB {
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
	CREATE UNIQUE INDEX idx_kb_documents_memory_key ON kb_documents(memory_key) WHERE memory_key != '';

	CREATE TABLE kb_chunks (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    document_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
	    chunk_index INTEGER NOT NULL DEFAULT 0,
	    content     TEXT    NOT NULL,
	    embedding   BLOB,
	    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
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

// TestUpsertMemoryByKey is a regression test for the partial-index ON CONFLICT
// bug: SQLite requires the conflict target to repeat the partial index's WHERE
// predicate ("WHERE memory_key != ”"), otherwise it errors with
// "ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint".
func TestUpsertMemoryByKey(t *testing.T) {
	db := openTestKBDB(t)
	// No embedder/proxy: UpsertMemory's keyed direct-DB path does not need them.
	store := NewKBStore(db, nil, 0, 0)
	ctx := context.Background()

	opts := MemoryUpsertOptions{
		FilePath:       "memory/project/test.md",
		Content:        "first version",
		Key:            "pando.test.key",
		Scope:          "project/",
		Source:         "user",
		Importance:     0.8,
		DefaultTTLDays: 180,
	}

	created, err := store.UpsertMemory(ctx, opts)
	if err != nil {
		t.Fatalf("first UpsertMemory error = %v", err)
	}
	if !created {
		t.Fatalf("first UpsertMemory: expected created=true")
	}

	// Second upsert with the same key must update, not error.
	opts.Content = "second version"
	created, err = store.UpsertMemory(ctx, opts)
	if err != nil {
		t.Fatalf("second UpsertMemory error = %v", err)
	}
	if created {
		t.Fatalf("second UpsertMemory: expected created=false (update)")
	}

	// Verify the row was updated in place (single row, new content, hits bumped).
	doc, err := store.GetMemoryByKey(ctx, opts.Key)
	if err != nil {
		t.Fatalf("GetMemoryByKey error = %v", err)
	}
	if doc == nil {
		t.Fatalf("GetMemoryByKey returned nil")
	}
	if doc.Content != "second version" {
		t.Errorf("content = %q, want %q", doc.Content, "second version")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_documents WHERE memory_key = ?`, opts.Key).Scan(&count); err != nil {
		t.Fatalf("count query error = %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}

func TestUpsertMemoryByKeyDefaultsSource(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, nil, 0, 0)
	ctx := context.Background()

	created, err := store.UpsertMemory(ctx, MemoryUpsertOptions{
		FilePath:       "memory/user/default-source.md",
		Content:        "memory without explicit source",
		Key:            "pando.test.default-source",
		Scope:          "user/",
		Importance:     0.5,
		DefaultTTLDays: 180,
	})
	if err != nil {
		t.Fatalf("UpsertMemory without source error = %v", err)
	}
	if !created {
		t.Fatalf("UpsertMemory without source: expected created=true")
	}

	doc, err := store.GetMemoryByKey(ctx, "pando.test.default-source")
	if err != nil {
		t.Fatalf("GetMemoryByKey error = %v", err)
	}
	if doc == nil {
		t.Fatalf("GetMemoryByKey returned nil")
	}
	if doc.Source != "memory" {
		t.Fatalf("source = %q, want %q", doc.Source, "memory")
	}
	if doc.Content != "memory without explicit source" {
		t.Fatalf("content = %q, want %q", doc.Content, "memory without explicit source")
	}
	if doc.MemoryScope != "user/" {
		t.Fatalf("scope = %q, want %q", doc.MemoryScope, "user/")
	}
	if doc.MemoryKey != "pando.test.default-source" {
		t.Fatalf("key = %q, want %q", doc.MemoryKey, "pando.test.default-source")
	}

	var source string
	if err := db.QueryRow(`SELECT source FROM kb_documents WHERE memory_key = ?`, "pando.test.default-source").Scan(&source); err != nil {
		t.Fatalf("source query error = %v", err)
	}
	if source != "memory" {
		t.Fatalf("stored source = %q, want %q", source, "memory")
	}
}
