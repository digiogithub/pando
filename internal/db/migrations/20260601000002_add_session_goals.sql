-- +goose Up
CREATE TABLE IF NOT EXISTS session_goals (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    objective TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    iteration INTEGER NOT NULL DEFAULT 0,
    max_iterations INTEGER NOT NULL DEFAULT 20,
    max_duration_seconds INTEGER NOT NULL DEFAULT 3600,
    started_at INTEGER NOT NULL,
    completed_at INTEGER,
    last_progress TEXT,
    next_step TEXT,
    blocked_reason TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_session_goals_session_id ON session_goals(session_id);

-- +goose Down
DROP INDEX IF EXISTS idx_session_goals_session_id;
DROP TABLE IF EXISTS session_goals;