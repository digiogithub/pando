-- +goose Up
-- +goose StatementBegin

-- Design artifacts (internal/design). The files of an artifact live in the
-- user's working tree (designer/<slug>/ by default) and their history lives in
-- scoped snapshots; SQLite only holds the metadata needed to list, resolve and
-- navigate them.
--
-- session_id is NOT a foreign key: an artifact is a committable deliverable in
-- the repository and must outlive the session that produced it.
CREATE TABLE IF NOT EXISTS design_artifacts (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL DEFAULT '',
    project_id      TEXT NOT NULL DEFAULT '',
    title           TEXT NOT NULL DEFAULT '',
    slug            TEXT NOT NULL,
    dir             TEXT NOT NULL,
    kind            TEXT NOT NULL DEFAULT 'web',
    skill_id        TEXT NOT NULL DEFAULT '',
    design_system   TEXT NOT NULL DEFAULT '',
    current_version INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_design_artifacts_dir ON design_artifacts(dir);
CREATE INDEX IF NOT EXISTS idx_design_artifacts_session ON design_artifacts(session_id);
CREATE INDEX IF NOT EXISTS idx_design_artifacts_updated ON design_artifacts(updated_at DESC);

-- One row per accepted iteration. snapshot_id points at a directory-scoped
-- snapshot (internal/snapshot, type "scoped"), which is what checkout reverts.
CREATE TABLE IF NOT EXISTS design_versions (
    artifact_id TEXT NOT NULL REFERENCES design_artifacts(id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    snapshot_id TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    PRIMARY KEY (artifact_id, number)
);

-- Structure index produced by the inspector after a render. Rebuilt per render,
-- so it is always deleted by (artifact_id, version) before being repopulated.
CREATE TABLE IF NOT EXISTS design_nodes (
    artifact_id TEXT NOT NULL REFERENCES design_artifacts(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    node_id     TEXT NOT NULL,
    parent_id   TEXT NOT NULL DEFAULT '',
    selector    TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL DEFAULT '',
    text        TEXT NOT NULL DEFAULT '',
    slide       INTEGER NOT NULL DEFAULT 0,
    box_x       REAL NOT NULL DEFAULT 0,
    box_y       REAL NOT NULL DEFAULT 0,
    box_w       REAL NOT NULL DEFAULT 0,
    box_h       REAL NOT NULL DEFAULT 0,
    styles      TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (artifact_id, version, node_id)
);

CREATE INDEX IF NOT EXISTS idx_design_nodes_slide ON design_nodes(artifact_id, version, slide);

-- Critic passes. Kept in their own table (not a column on design_versions) so a
-- version can be re-critiqued without rewriting its history row.
CREATE TABLE IF NOT EXISTS design_critiques (
    id          TEXT PRIMARY KEY,
    artifact_id TEXT NOT NULL REFERENCES design_artifacts(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    score       REAL NOT NULL DEFAULT 0,
    summary     TEXT NOT NULL DEFAULT '',
    issues      TEXT NOT NULL DEFAULT '[]',
    created_at  INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_design_critiques_version ON design_critiques(artifact_id, version);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_design_critiques_version;
DROP TABLE IF EXISTS design_critiques;
DROP INDEX IF EXISTS idx_design_nodes_slide;
DROP TABLE IF EXISTS design_nodes;
DROP TABLE IF EXISTS design_versions;
DROP INDEX IF EXISTS idx_design_artifacts_updated;
DROP INDEX IF EXISTS idx_design_artifacts_session;
DROP INDEX IF EXISTS idx_design_artifacts_dir;
DROP TABLE IF EXISTS design_artifacts;

-- +goose StatementEnd
