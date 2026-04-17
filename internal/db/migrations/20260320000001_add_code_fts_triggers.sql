-- +goose Up
-- +goose StatementBegin

-- Triggers to keep code_symbols_fts in sync with code_symbols.
-- The FTS5 external content table does not auto-populate on INSERT/UPDATE/DELETE.

CREATE TRIGGER IF NOT EXISTS code_symbols_fts_ai
  AFTER INSERT ON code_symbols BEGIN
    INSERT INTO code_symbols_fts(rowid, name, name_path, doc_string, source_code)
    VALUES (new.rowid, new.name, new.name_path, new.doc_string, new.source_code);
  END;

CREATE TRIGGER IF NOT EXISTS code_symbols_fts_ad
  AFTER DELETE ON code_symbols BEGIN
    INSERT INTO code_symbols_fts(code_symbols_fts, rowid, name, name_path, doc_string, source_code)
    VALUES ('delete', old.rowid, old.name, old.name_path, old.doc_string, old.source_code);
  END;

CREATE TRIGGER IF NOT EXISTS code_symbols_fts_au
  AFTER UPDATE ON code_symbols BEGIN
    INSERT INTO code_symbols_fts(code_symbols_fts, rowid, name, name_path, doc_string, source_code)
    VALUES ('delete', old.rowid, old.name, old.name_path, old.doc_string, old.source_code);
    INSERT INTO code_symbols_fts(rowid, name, name_path, doc_string, source_code)
    VALUES (new.rowid, new.name, new.name_path, new.doc_string, new.source_code);
  END;

-- Rebuild FTS index from any existing data in code_symbols.
INSERT INTO code_symbols_fts(code_symbols_fts) VALUES('rebuild');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS code_symbols_fts_au;
DROP TRIGGER IF EXISTS code_symbols_fts_ad;
DROP TRIGGER IF EXISTS code_symbols_fts_ai;

-- +goose StatementEnd
