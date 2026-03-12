-- +goose Up
-- +goose StatementBegin

-- code_projects stores indexed code projects
CREATE TABLE IF NOT EXISTS code_projects (
    project_id      TEXT PRIMARY KEY,
    name            TEXT NOT NULL DEFAULT '',
    root_path       TEXT NOT NULL,
    language_stats  TEXT NOT NULL DEFAULT '{}',
    last_indexed_at DATETIME,
    indexing_status TEXT NOT NULL DEFAULT 'pending',
    created_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- code_files tracks indexed source files with hash-based change detection
CREATE TABLE IF NOT EXISTS code_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id    TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
    file_path     TEXT NOT NULL,
    language      TEXT NOT NULL,
    file_hash     TEXT NOT NULL,
    symbols_count INTEGER NOT NULL DEFAULT 0,
    indexed_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(project_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_code_files_project ON code_files(project_id);
CREATE INDEX IF NOT EXISTS idx_code_files_language ON code_files(project_id, language);

-- code_symbols stores extracted code symbols
CREATE TABLE IF NOT EXISTS code_symbols (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
    file_id     INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    language    TEXT NOT NULL,
    symbol_type TEXT NOT NULL,
    name        TEXT NOT NULL,
    name_path   TEXT NOT NULL,
    start_line  INTEGER NOT NULL DEFAULT 0,
    end_line    INTEGER NOT NULL DEFAULT 0,
    start_byte  INTEGER NOT NULL DEFAULT 0,
    end_byte    INTEGER NOT NULL DEFAULT 0,
    source_code TEXT NOT NULL DEFAULT '',
    signature   TEXT NOT NULL DEFAULT '',
    doc_string  TEXT NOT NULL DEFAULT '',
    parent_id   TEXT,
    metadata    TEXT NOT NULL DEFAULT '{}',
    -- embedding is a little-endian blob of float32 values (4 bytes each)
    embedding   BLOB,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_code_symbols_project   ON code_symbols(project_id);
CREATE INDEX IF NOT EXISTS idx_code_symbols_file      ON code_symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_code_symbols_name      ON code_symbols(project_id, name);
CREATE INDEX IF NOT EXISTS idx_code_symbols_name_path ON code_symbols(project_id, name_path);
CREATE INDEX IF NOT EXISTS idx_code_symbols_type      ON code_symbols(project_id, symbol_type);
CREATE INDEX IF NOT EXISTS idx_code_symbols_lang      ON code_symbols(project_id, language);
CREATE INDEX IF NOT EXISTS idx_code_symbols_parent    ON code_symbols(parent_id);

-- code_symbols_fts is a FTS5 virtual table for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS code_symbols_fts USING fts5(
    name,
    name_path,
    doc_string,
    source_code,
    content       = 'code_symbols',
    content_rowid = 'rowid',
    tokenize      = 'porter unicode61'
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS code_symbols_fts;
DROP INDEX IF EXISTS idx_code_symbols_parent;
DROP INDEX IF EXISTS idx_code_symbols_lang;
DROP INDEX IF EXISTS idx_code_symbols_type;
DROP INDEX IF EXISTS idx_code_symbols_name_path;
DROP INDEX IF EXISTS idx_code_symbols_name;
DROP INDEX IF EXISTS idx_code_symbols_file;
DROP INDEX IF EXISTS idx_code_symbols_project;
DROP TABLE IF EXISTS code_symbols;
DROP INDEX IF EXISTS idx_code_files_language;
DROP INDEX IF EXISTS idx_code_files_project;
DROP TABLE IF EXISTS code_files;
DROP TABLE IF EXISTS code_projects;

-- +goose StatementEnd
