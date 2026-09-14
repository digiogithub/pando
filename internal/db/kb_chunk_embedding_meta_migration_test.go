package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/pressly/goose/v3"
)

// TestMigrationsBackfillKBChunkEmbeddingDims runs the real migration chain
// against a database that already has a kb_chunks row with an embedding blob
// (simulating an upgrade from before PANDO-US-0029), so the backfill this
// migration performs is exercised the same way a real upgrade would hit it,
// and so a broken migration fails here instead of at a user's first start.
func TestMigrationsBackfillKBChunkEmbeddingDims(t *testing.T) {
	const preMigrationVersion = 20260713000001 // 20260713000001_add_kb_links.sql

	dbPath := filepath.Join(t.TempDir(), "pando.db")
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	goose.SetBaseFS(FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}

	// Apply every migration up to (but not including) the one under test, so
	// a pre-existing chunk with an embedding blob exists before it runs —
	// exactly the upgrade scenario the backfill must handle.
	if err := goose.UpTo(conn, "migrations", preMigrationVersion); err != nil {
		t.Fatalf("goose up to pre-migration: %v", err)
	}

	if _, err := conn.Exec(`INSERT INTO kb_documents (file_path, content) VALUES ('docs/a.md', 'body')`); err != nil {
		t.Fatalf("insert document: %v", err)
	}
	// 4 little-endian float32 values = 16 bytes (internal/rag/store.go).
	blob := make([]byte, 16)
	if _, err := conn.Exec(`INSERT INTO kb_chunks (document_id, chunk_index, content, embedding) VALUES (1, 0, 'chunk', ?)`, blob); err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO kb_chunks (document_id, chunk_index, content, embedding) VALUES (1, 1, 'chunk no embedding', NULL)`); err != nil {
		t.Fatalf("insert chunk without embedding: %v", err)
	}

	// Now apply the rest of the chain, including the migration under test.
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	var dims int
	if err := conn.QueryRow(`SELECT embedding_dims FROM kb_chunks WHERE chunk_index = 0`).Scan(&dims); err != nil {
		t.Fatalf("select embedding_dims: %v", err)
	}
	if dims != 4 {
		t.Fatalf("embedding_dims = %d, want 4 (16-byte blob / 4 bytes per float32)", dims)
	}

	var model string
	if err := conn.QueryRow(`SELECT embedding_model FROM kb_chunks WHERE chunk_index = 0`).Scan(&model); err != nil {
		t.Fatalf("select embedding_model: %v", err)
	}
	if model != "" {
		t.Fatalf("embedding_model = %q, want empty (unknown) for a pre-existing row", model)
	}

	var dimsNoEmbedding int
	if err := conn.QueryRow(`SELECT embedding_dims FROM kb_chunks WHERE chunk_index = 1`).Scan(&dimsNoEmbedding); err != nil {
		t.Fatalf("select embedding_dims (no embedding): %v", err)
	}
	if dimsNoEmbedding != 0 {
		t.Fatalf("embedding_dims (no embedding) = %d, want 0 (default, not backfilled)", dimsNoEmbedding)
	}

	// Idempotent: running the full chain again on an already-migrated
	// database must not error (goose no-ops on already-applied migrations).
	if err := goose.Up(conn, "migrations"); err != nil {
		t.Fatalf("goose up (second run) error = %v", err)
	}

	// Reversible: Down must not error even though the columns themselves are
	// left in place (SQLite cannot drop columns on older builds) — the index
	// it adds must actually go away.
	if err := goose.DownTo(conn, "migrations", preMigrationVersion); err != nil {
		t.Fatalf("goose down: %v", err)
	}
	err = conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_kb_chunks_embedding_dims'`).Scan(new(string))
	if err != sql.ErrNoRows {
		t.Fatalf("index idx_kb_chunks_embedding_dims still present after Down, err = %v", err)
	}
}
