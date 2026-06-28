-- +goose Up
-- +goose StatementBegin

-- Stop storing source code text in the code index. The text lives on disk and
-- is hydrated on demand from file_path + byte/line offsets. The FTS5 index is
-- converted from external-content (which reads the base table) to CONTENTLESS
-- with contentless_delete=1 so it keeps the inverted index for BM25 search but
-- no longer needs the source_code column to exist.
--
-- ORDER MATTERS: repopulate the new FTS index from the still-present source_code
-- BEFORE dropping the column, so already-indexed symbols stay searchable with no
-- mandatory reindex.

CREATE VIRTUAL TABLE IF NOT EXISTS code_symbols_fts_new USING fts5(
    name,
    name_path,
    doc_string,
    source_code,
    content            = '',
    contentless_delete = 1,
    tokenize           = 'porter unicode61'
);

INSERT INTO code_symbols_fts_new(rowid, name, name_path, doc_string, source_code)
    SELECT rowid, name, name_path, doc_string, source_code FROM code_symbols;

-- The old external-content triggers read new/old.source_code; they cannot
-- survive the column drop. Replace them with a single rowid-only delete trigger
-- (contentless_delete needs no column values). INSERTs into the FTS index are
-- now performed explicitly by the indexer within its transaction.
DROP TRIGGER IF EXISTS code_symbols_fts_ai;
DROP TRIGGER IF EXISTS code_symbols_fts_au;
DROP TRIGGER IF EXISTS code_symbols_fts_ad;

DROP TABLE IF EXISTS code_symbols_fts;
ALTER TABLE code_symbols_fts_new RENAME TO code_symbols_fts;

ALTER TABLE code_symbols DROP COLUMN source_code;

-- A contentless_delete=1 FTS5 table supports ordinary DELETE statements
-- (the legacy 'delete' command is only for external/standard contentless tables).
CREATE TRIGGER code_symbols_fts_ad
  AFTER DELETE ON code_symbols BEGIN
    DELETE FROM code_symbols_fts WHERE rowid = old.rowid;
  END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- NOTE: this Down is lossy. The original source code text was removed from the
-- database and cannot be recovered here; the restored column is empty. A full
-- reindex is required to repopulate symbol bodies after a downgrade.

ALTER TABLE code_symbols ADD COLUMN source_code TEXT NOT NULL DEFAULT '';

DROP TRIGGER IF EXISTS code_symbols_fts_ad;
DROP TABLE IF EXISTS code_symbols_fts;

CREATE VIRTUAL TABLE code_symbols_fts USING fts5(
    name,
    name_path,
    doc_string,
    source_code,
    content       = 'code_symbols',
    content_rowid = 'rowid',
    tokenize      = 'porter unicode61'
);

CREATE TRIGGER code_symbols_fts_ai
  AFTER INSERT ON code_symbols BEGIN
    INSERT INTO code_symbols_fts(rowid, name, name_path, doc_string, source_code)
    VALUES (new.rowid, new.name, new.name_path, new.doc_string, new.source_code);
  END;

CREATE TRIGGER code_symbols_fts_ad
  AFTER DELETE ON code_symbols BEGIN
    INSERT INTO code_symbols_fts(code_symbols_fts, rowid, name, name_path, doc_string, source_code)
    VALUES ('delete', old.rowid, old.name, old.name_path, old.doc_string, old.source_code);
  END;

CREATE TRIGGER code_symbols_fts_au
  AFTER UPDATE ON code_symbols BEGIN
    INSERT INTO code_symbols_fts(code_symbols_fts, rowid, name, name_path, doc_string, source_code)
    VALUES ('delete', old.rowid, old.name, old.name_path, old.doc_string, old.source_code);
    INSERT INTO code_symbols_fts(rowid, name, name_path, doc_string, source_code)
    VALUES (new.rowid, new.name, new.name_path, new.doc_string, new.source_code);
  END;

INSERT INTO code_symbols_fts(code_symbols_fts) VALUES('rebuild');

-- +goose StatementEnd
