-- +goose Up
-- +goose StatementBegin

-- Columns used by the rag library API (kb.Put, kb.ApplyChanges, kb.Search).
-- All additive with defaults: rows written by older code keep working and
-- read as kind 'document', status 'active', no author/rev/blob, no lines.
ALTER TABLE kb_documents ADD COLUMN kind     TEXT NOT NULL DEFAULT 'document';
ALTER TABLE kb_documents ADD COLUMN status   TEXT NOT NULL DEFAULT 'active';
ALTER TABLE kb_documents ADD COLUMN author   TEXT NOT NULL DEFAULT '';
-- rev is the caller's opaque revision (sha256 of the content when not given);
-- git_blob is the git blob id the content was read from, when known.
ALTER TABLE kb_documents ADD COLUMN rev      TEXT NOT NULL DEFAULT '';
ALTER TABLE kb_documents ADD COLUMN git_blob TEXT NOT NULL DEFAULT '';

-- 1-based line range of each chunk in the source document (0 = unknown).
ALTER TABLE kb_chunks ADD COLUMN start_line INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_chunks ADD COLUMN end_line   INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_kb_documents_kind_status ON kb_documents(kind, status);

-- Keyed memories are documents of kind 'memory'. Existing ones are backfilled
-- here, and the trigger tags the ones inserted later by code that does not
-- know about the column.
UPDATE kb_documents SET kind = 'memory' WHERE memory_key != '';

CREATE TRIGGER IF NOT EXISTS kb_documents_memory_kind
AFTER INSERT ON kb_documents
WHEN NEW.memory_key != '' AND NEW.kind = 'document'
BEGIN
    UPDATE kb_documents SET kind = 'memory' WHERE id = NEW.id;
END;

-- kb_state holds per-store values, such as the ApplyChanges watermark.
CREATE TABLE IF NOT EXISTS kb_state (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- kb_doc_fields_fts indexes the front-matter fields of a document (rowid =
-- kb_documents.id) so a search can match a title, label or alias that is not
-- in the body text.
CREATE VIRTUAL TABLE IF NOT EXISTS kb_doc_fields_fts USING fts5(
    title,
    labels,
    aliases,
    tokenize = 'porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS kb_documents_fields_fts_delete
AFTER DELETE ON kb_documents
BEGIN
    DELETE FROM kb_doc_fields_fts WHERE rowid = OLD.id;
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS kb_documents_fields_fts_delete;
DROP TABLE IF EXISTS kb_doc_fields_fts;
DROP TABLE IF EXISTS kb_state;
DROP TRIGGER IF EXISTS kb_documents_memory_kind;
DROP INDEX IF EXISTS idx_kb_documents_kind_status;

-- SQLite does not support DROP COLUMN in older versions; columns are left in place on rollback.

-- +goose StatementEnd
