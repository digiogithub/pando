-- +goose Up
-- +goose StatementBegin

ALTER TABLE kb_documents ADD COLUMN memory_key   TEXT;
ALTER TABLE kb_documents ADD COLUMN memory_scope  TEXT;
ALTER TABLE kb_documents ADD COLUMN outdated      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_documents ADD COLUMN expires_at    DATETIME;
ALTER TABLE kb_documents ADD COLUMN hits          INTEGER NOT NULL DEFAULT 0;
ALTER TABLE kb_documents ADD COLUMN importance    REAL    NOT NULL DEFAULT 0.5;
ALTER TABLE kb_documents ADD COLUMN source        TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_documents_memory_key ON kb_documents(memory_key) WHERE memory_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_kb_documents_expires ON kb_documents(expires_at) WHERE expires_at IS NOT NULL AND outdated = 0;
CREATE INDEX IF NOT EXISTS idx_kb_documents_scope ON kb_documents(memory_scope) WHERE memory_scope IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_kb_documents_scope;
DROP INDEX IF EXISTS idx_kb_documents_expires;
DROP INDEX IF EXISTS idx_kb_documents_memory_key;

-- memory columns cannot be dropped in SQLite

-- +goose StatementEnd
