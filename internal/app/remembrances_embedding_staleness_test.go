package app

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// stalenessDimEmbedder returns a fixed-dimension, fixed-direction vector
// regardless of content, enough to control the configured embedder's
// dimension without a real provider.
type stalenessDimEmbedder struct{ dim int }

func (e stalenessDimEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, e.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

func (e stalenessDimEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	v := make([]float32, e.dim)
	v[0] = 1
	return v, nil
}

func (e stalenessDimEmbedder) Dimension() int { return e.dim }

// openStalenessTestDB creates an in-memory SQLite DB with the KB schema
// mirroring production migrations through
// 20260914000001_add_kb_chunk_embedding_meta.sql.
func openStalenessTestDB(t *testing.T) *sql.DB {
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
	return db
}

// captureLogs redirects slog's default logger to a buffer for the duration
// of the test and restores the previous default afterwards, so assertions
// can inspect exactly what logging.WarnPersist/Error emitted.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	return &buf
}

// TestInitKBEmbeddingStalenessCheck_WarnsOnMismatch is a regression test for
// PANDO-US-0029: the startup check must log one warning naming the
// configured model, the recorded model, and the count of mismatched chunks.
func TestInitKBEmbeddingStalenessCheck_WarnsOnMismatch(t *testing.T) {
	db := openStalenessTestDB(t)
	ctx := context.Background()

	store := kb.NewKBStore(db, stalenessDimEmbedder{dim: 3}, 0, 0)
	store.SetEmbeddingModel("model-v1")
	if err := store.AddDocument(ctx, "docs/old.md", "content", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	svc := &rag.RemembrancesService{KB: store}
	svc.SetDocumentEmbedder(stalenessDimEmbedder{dim: 5})
	cfg := &config.RemembrancesConfig{DocumentEmbeddingModel: "model-v2"}

	buf := captureLogs(t)
	a := &App{}
	a.initKBEmbeddingStalenessCheck(ctx, svc, cfg)

	out := buf.String()
	if !strings.Contains(out, "model-v1") {
		t.Fatalf("log output missing recorded model %q, got: %s", "model-v1", out)
	}
	if !strings.Contains(out, "model-v2") {
		t.Fatalf("log output missing configured model %q, got: %s", "model-v2", out)
	}
	if !strings.Contains(out, "1") {
		t.Fatalf("log output missing the mismatch count, got: %s", out)
	}
	if !strings.Contains(strings.ToUpper(out), "WARN") {
		t.Fatalf("expected a WARN-level log line, got: %s", out)
	}
}

// TestInitKBEmbeddingStalenessCheck_SilentOnConsistentCorpus asserts a
// consistent corpus (recorded dimension matches the configured embedder)
// logs nothing at all.
func TestInitKBEmbeddingStalenessCheck_SilentOnConsistentCorpus(t *testing.T) {
	db := openStalenessTestDB(t)
	ctx := context.Background()

	store := kb.NewKBStore(db, stalenessDimEmbedder{dim: 3}, 0, 0)
	store.SetEmbeddingModel("model-v1")
	if err := store.AddDocument(ctx, "docs/a.md", "content", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	svc := &rag.RemembrancesService{KB: store}
	svc.SetDocumentEmbedder(stalenessDimEmbedder{dim: 3})
	cfg := &config.RemembrancesConfig{DocumentEmbeddingModel: "model-v1"}

	buf := captureLogs(t)
	a := &App{}
	a.initKBEmbeddingStalenessCheck(ctx, svc, cfg)

	if buf.Len() != 0 {
		t.Fatalf("expected no log output on a consistent corpus, got: %s", buf.String())
	}
}

// TestInitKBEmbeddingStalenessCheck_NilInputsDoNotPanic covers the guard
// clauses: no service, no KB store, no embedder, or a zero/unset dimension.
func TestInitKBEmbeddingStalenessCheck_NilInputsDoNotPanic(t *testing.T) {
	a := &App{}
	ctx := context.Background()
	cfg := &config.RemembrancesConfig{DocumentEmbeddingModel: "model-v1"}

	a.initKBEmbeddingStalenessCheck(ctx, nil, cfg)
	a.initKBEmbeddingStalenessCheck(ctx, &rag.RemembrancesService{}, cfg)
	a.initKBEmbeddingStalenessCheck(ctx, &rag.RemembrancesService{KB: kb.NewKBStore(openStalenessTestDB(t), nil, 0, 0)}, cfg)
}
