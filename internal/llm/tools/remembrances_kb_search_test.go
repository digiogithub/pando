package tools

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/kb"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// openKBSearchTestStore builds an in-memory KB store with the full schema
// search needs (documents + chunks + FTS5), mirroring production migrations
// through 20260914000001_add_kb_chunk_embedding_meta.sql. It returns the
// store and the underlying DB, so a test can open a second KBStore on the
// same database with a different embedder (simulating an embedder swap).
func openKBSearchTestStore(t *testing.T, embedder embeddings.Embedder) (*kb.KBStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
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
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema setup error = %v", err)
	}
	return kb.NewKBStore(db, embedder, 0, 0), db
}

// kbSearchTestEmbedder returns a fixed-dimension, fixed-direction vector
// regardless of content, enough to control dimension for the stale-embedding
// warning test without a real provider.
type kbSearchTestEmbedder struct{ dim int }

func (e kbSearchTestEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (e kbSearchTestEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, e.dim)
	v[0] = 1
	return v, nil
}

func (e kbSearchTestEmbedder) Dimension() int { return e.dim }

// TestKBSearchDocumentsToolWarnsOnStaleEmbeddingDimension is a regression
// test for PANDO-US-0029: a kb_search_documents response that skipped chunks
// for a dimension mismatch must carry a warning with the skipped count; a
// clean query must carry none.
func TestKBSearchDocumentsToolWarnsOnStaleEmbeddingDimension(t *testing.T) {
	store, db := openKBSearchTestStore(t, kbSearchTestEmbedder{dim: 3})
	store.SetEmbeddingModel("model-v1")
	ctx := context.Background()

	if err := store.AddDocument(ctx, "docs/old.md", "marker document alpha", nil); err != nil {
		t.Fatalf("AddDocument(old) error = %v", err)
	}

	// A clean query on a consistent corpus carries no warning. The tool
	// renders its structured output (TOON/TOML/JSON, whichever
	// FormatStructuredData picks) into resp.Content, so this checks for the
	// warning text rather than assuming a particular serialization.
	cleanTool := NewKBSearchDocumentsTool(store)
	cleanResp, err := cleanTool.Run(ctx, ToolCall{Input: `{"query":"marker document alpha"}`})
	if err != nil {
		t.Fatalf("tool.Run() (clean) error = %v", err)
	}
	if cleanResp.IsError {
		t.Fatalf("tool.Run() (clean) returned error response: %s", cleanResp.Content)
	}
	if strings.Contains(cleanResp.Content, "stale embedding") {
		t.Fatalf("content = %q on a consistent corpus, want no stale-embedding warning", cleanResp.Content)
	}

	// Simulate an embedder reconfiguration: a second store on the same
	// database, with a different embedder dimension and model, writes a new
	// document. The first document's chunk is now stale relative to it.
	store2 := kb.NewKBStore(db, kbSearchTestEmbedder{dim: 5}, 0, 0)
	store2.SetEmbeddingModel("model-v2")
	if err := store2.AddDocument(ctx, "docs/new.md", "marker document beta", nil); err != nil {
		t.Fatalf("AddDocument(new) error = %v", err)
	}

	// A query against the now-5-dim-configured store scans both chunks: the
	// old 3-dim one is skipped for the mismatch and must be reported.
	tool := NewKBSearchDocumentsTool(store2)
	resp, err := tool.Run(ctx, ToolCall{Input: `{"query":"marker document"}`})
	if err != nil {
		t.Fatalf("tool.Run() error = %v", err)
	}
	if resp.IsError {
		t.Fatalf("tool.Run() returned error response: %s", resp.Content)
	}

	if !strings.Contains(resp.Content, "stale embedding") {
		t.Fatalf("content = %q, want a stale-embedding warning", resp.Content)
	}
	if !strings.Contains(resp.Content, "skipped 1 chunk") {
		t.Fatalf("content = %q, want the skipped count (1)", resp.Content)
	}
	if !strings.Contains(resp.Content, "docs/new.md") {
		t.Fatalf("content = %q, want the matching-dimension document docs/new.md", resp.Content)
	}
}
