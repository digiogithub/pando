-- +goose Up
-- +goose StatementBegin

-- Add embedding provenance columns to kb_chunks so a document-embedder change
-- can be detected instead of silently degrading recall (PANDO-US-0029).
-- embedding_model is left empty ('') for rows written before this migration:
-- empty is treated as "unknown", not a mismatch, by the startup staleness
-- check and by kb_search_documents' skipped-chunk warning. embedding_dims is
-- backfilled from the stored blob length (little-endian float32, 4 bytes per
-- value — internal/rag/store.go) so existing embeddings are immediately
-- comparable against the configured embedder's dimension; chunks with no
-- embedding keep embedding_dims = 0, its default.
ALTER TABLE kb_chunks ADD COLUMN embedding_model TEXT    NOT NULL DEFAULT '';
ALTER TABLE kb_chunks ADD COLUMN embedding_dims  INTEGER NOT NULL DEFAULT 0;

UPDATE kb_chunks SET embedding_dims = length(embedding) / 4 WHERE embedding IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_kb_chunks_embedding_dims ON kb_chunks(embedding_dims);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_kb_chunks_embedding_dims;

-- SQLite does not support DROP COLUMN in older versions; columns are left in place on rollback.

-- +goose StatementEnd
