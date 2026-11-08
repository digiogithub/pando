-- +goose Up
-- +goose StatementBegin

-- Add memory system columns to kb_documents.
-- All columns are nullable/have defaults so existing rows continue to work.
ALTER TABLE kb_documents ADD COLUMN memory_key   TEXT    NOT NULL DEFAULT '';
ALTER TABLE kb_documents ADD COLUMN memory_scope TEXT    NOT NULL DEFAULT '';
ALTER TABLE kb_documents ADD COLUMN outdated     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_documents ADD COLUMN expires_at   DATETIME;
ALTER TABLE kb_documents ADD COLUMN hits         INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_documents ADD COLUMN importance   REAL    NOT NULL DEFAULT 0.5;
ALTER TABLE kb_documents ADD COLUMN source       TEXT    NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_documents_memory_key ON kb_documents(memory_key) WHERE memory_key != '';
CREATE INDEX IF NOT EXISTS idx_kb_documents_memory_scope ON kb_documents(memory_scope) WHERE memory_scope != '';
CREATE INDEX IF NOT EXISTS idx_kb_documents_outdated ON kb_documents(outdated);
CREATE INDEX IF NOT EXISTS idx_kb_documents_expires_at ON kb_documents(expires_at) WHERE expires_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_kb_documents_expires_at;
DROP INDEX IF EXISTS idx_kb_documents_outdated;
DROP INDEX IF EXISTS idx_kb_documents_memory_scope;
DROP INDEX IF EXISTS idx_kb_documents_memory_key;

-- SQLite does not support DROP COLUMN in older versions; columns are left in place on rollback.

-- +goose StatementEnd
