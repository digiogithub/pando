-- +goose Up
-- +goose StatementBegin

-- rag_chunks stores document chunks with original text, optional embedding vector,
-- and arbitrary JSON metadata.
--
-- Vector similarity search is performed in Go (pure, no SQLite extension required).
-- Full-text search is provided by rag_fts using SQLite's built-in FTS5 engine.
CREATE TABLE IF NOT EXISTS rag_chunks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    collection  TEXT    NOT NULL DEFAULT 'default',
    source      TEXT    NOT NULL DEFAULT '',
    content     TEXT    NOT NULL,
    chunk_index INTEGER NOT NULL DEFAULT 0,
    metadata    TEXT    NOT NULL DEFAULT '{}',
    -- embedding is a little-endian blob of float32 values (4 bytes each).
    -- NULL means no embedding has been stored for this chunk.
    embedding   BLOB,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_rag_chunks_collection     ON rag_chunks(collection);
CREATE INDEX IF NOT EXISTS idx_rag_chunks_source         ON rag_chunks(source);
CREATE INDEX IF NOT EXISTS idx_rag_chunks_col_idx        ON rag_chunks(collection, chunk_index);

-- rag_fts is a FTS5 external-content virtual table backed by rag_chunks.
-- SQLite's FTS5 is compiled into the ncruces WASM binary, no extra extension needed.
-- content= avoids duplicating text; content_rowid= maps FTS5 rowids to rag_chunks.id.
-- The application must keep this index in sync on insert/update/delete.
CREATE VIRTUAL TABLE IF NOT EXISTS rag_fts USING fts5(
    content,
    source,
    collection,
    content     = 'rag_chunks',
    content_rowid = 'id'
);

-- rag_meta stores per-store key-value configuration (e.g. embedding dimension).
CREATE TABLE IF NOT EXISTS rag_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS rag_fts;
DROP INDEX IF EXISTS idx_rag_chunks_col_idx;
DROP INDEX IF EXISTS idx_rag_chunks_source;
DROP INDEX IF EXISTS idx_rag_chunks_collection;
DROP TABLE IF EXISTS rag_chunks;
DROP TABLE IF EXISTS rag_meta;

-- +goose StatementEnd
