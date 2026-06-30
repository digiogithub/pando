-- +goose Up
-- +goose StatementBegin

-- code_edges stores the code property-graph relationships extracted during
-- indexing (lean-ctx Phase 4). Each row is a directed edge from a source
-- (file + optional enclosing symbol) to a destination identified by name
-- (callee / referenced type) and/or import path. Destinations are kept as
-- unresolved names so cross-file/cross-package resolution stays a cheap query
-- at analysis time rather than a brittle index-time join.
CREATE TABLE IF NOT EXISTS code_edges (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id  TEXT NOT NULL REFERENCES code_projects(project_id) ON DELETE CASCADE,
    file_id     INTEGER NOT NULL REFERENCES code_files(id) ON DELETE CASCADE,
    edge_type   TEXT NOT NULL,                       -- imports | calls | type_ref
    src_file    TEXT NOT NULL DEFAULT '',            -- relative source file path
    src_symbol  TEXT NOT NULL DEFAULT '',            -- enclosing symbol id (calls/type_ref)
    dst_name    TEXT NOT NULL DEFAULT '',            -- callee / referenced type name
    dst_path    TEXT NOT NULL DEFAULT '',            -- import path (imports)
    start_line  INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_code_edges_project ON code_edges(project_id);
CREATE INDEX IF NOT EXISTS idx_code_edges_file    ON code_edges(file_id);
CREATE INDEX IF NOT EXISTS idx_code_edges_type    ON code_edges(project_id, edge_type);
CREATE INDEX IF NOT EXISTS idx_code_edges_dstname ON code_edges(project_id, dst_name);
CREATE INDEX IF NOT EXISTS idx_code_edges_dstpath ON code_edges(project_id, dst_path);
CREATE INDEX IF NOT EXISTS idx_code_edges_srcsym  ON code_edges(src_symbol);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_code_edges_srcsym;
DROP INDEX IF EXISTS idx_code_edges_dstpath;
DROP INDEX IF EXISTS idx_code_edges_dstname;
DROP INDEX IF EXISTS idx_code_edges_type;
DROP INDEX IF EXISTS idx_code_edges_file;
DROP INDEX IF EXISTS idx_code_edges_project;
DROP TABLE IF EXISTS code_edges;

-- +goose StatementEnd
