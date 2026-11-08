-- +goose Up
-- +goose StatementBegin

-- kb_documents stores document metadata and full content.
-- The content is chunked and stored in kb_chunks with embeddings.
CREATE TABLE IF NOT EXISTS kb_documents (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path  TEXT    NOT NULL UNIQUE,
    content    TEXT    NOT NULL,
    metadata   TEXT    NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- kb_chunks stores individual chunks of documents with embeddings.
-- Each chunk is linked to its parent document via document_id.
CREATE TABLE IF NOT EXISTS kb_chunks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    document_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL DEFAULT 0,
    content     TEXT    NOT NULL,
    -- embedding is a little-endian blob of float32 values (4 bytes each).
    -- NULL means no embedding has been stored for this chunk.
    embedding   BLOB,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_kb_chunks_document ON kb_chunks(document_id);
CREATE INDEX IF NOT EXISTS idx_kb_chunks_doc_idx  ON kb_chunks(document_id, chunk_index);

-- kb_fts is a FTS5 external-content virtual table backed by kb_chunks.
-- SQLite's FTS5 is compiled into the ncruces WASM binary, no extra extension needed.
-- content= avoids duplicating text; content_rowid= maps FTS5 rowids to kb_chunks.id.
-- The application must keep this index in sync on insert/update/delete.
CREATE VIRTUAL TABLE IF NOT EXISTS kb_fts USING fts5(
    content,
    content     = 'kb_chunks',
    content_rowid = 'id',
    tokenize    = 'porter unicode61'
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS kb_fts;
DROP INDEX IF EXISTS idx_kb_chunks_doc_idx;
DROP INDEX IF EXISTS idx_kb_chunks_document;
DROP TABLE IF EXISTS kb_chunks;
DROP TABLE IF EXISTS kb_documents;

-- +goose StatementEnd
