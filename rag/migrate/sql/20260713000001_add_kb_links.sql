-- +goose Up
-- +goose StatementBegin

-- kb_links stores the wiki-style [[link]] relations extracted from KB document
-- bodies. Targets are kept as UNRESOLVED normalized slugs (never a FK to the
-- destination document) so a link may point at a document that does not exist
-- yet, and so renames self-heal at query time. Resolution (slug -> document)
-- happens on read, mirroring the code_edges design.
CREATE TABLE IF NOT EXISTS kb_links (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    source_document_id INTEGER NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    source_path        TEXT    NOT NULL DEFAULT '',  -- denormalized file_path of the source doc
    target_slug        TEXT    NOT NULL,             -- normalized link target
    target_raw         TEXT    NOT NULL DEFAULT '',  -- target exactly as written
    label              TEXT    NOT NULL DEFAULT '',  -- display label of [[target|label]]
    position           INTEGER NOT NULL DEFAULT 0,   -- order of appearance in the body
    created_at         DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_kb_links_source      ON kb_links(source_document_id);
CREATE INDEX IF NOT EXISTS idx_kb_links_target      ON kb_links(target_slug);
CREATE INDEX IF NOT EXISTS idx_kb_links_source_path ON kb_links(source_path);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_kb_links_source_path;
DROP INDEX IF EXISTS idx_kb_links_target;
DROP INDEX IF EXISTS idx_kb_links_source;
DROP TABLE IF EXISTS kb_links;

-- +goose StatementEnd
