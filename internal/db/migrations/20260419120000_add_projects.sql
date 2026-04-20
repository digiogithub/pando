-- +goose Up
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    path        TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'stopped',
    initialized INTEGER NOT NULL DEFAULT 0,
    acp_pid     INTEGER,
    acp_port    INTEGER,
    last_opened INTEGER,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(path);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);

-- +goose Down
DROP INDEX IF EXISTS idx_projects_status;
DROP INDEX IF EXISTS idx_projects_path;
DROP TABLE IF EXISTS projects;
