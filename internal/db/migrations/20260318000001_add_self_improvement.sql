-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS prompt_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    section TEXT NOT NULL,
    content TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    is_active INTEGER NOT NULL DEFAULT 1,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(name, version)
);
CREATE INDEX IF NOT EXISTS idx_prompt_templates_name ON prompt_templates(name, is_active);

CREATE TABLE IF NOT EXISTS session_scores (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    template_id TEXT REFERENCES prompt_templates(id),
    reward REAL NOT NULL DEFAULT 0.0,
    success_score REAL NOT NULL DEFAULT 0.0,
    efficiency_score REAL NOT NULL DEFAULT 0.0,
    judge_analysis TEXT,
    judge_model TEXT,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    message_count INTEGER NOT NULL DEFAULT 0,
    user_corrections INTEGER NOT NULL DEFAULT 0,
    evaluated_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_scores_session ON session_scores(session_id);
CREATE INDEX IF NOT EXISTS idx_session_scores_template ON session_scores(template_id);

CREATE TABLE IF NOT EXISTS prompt_ucb_stats (
    template_id TEXT PRIMARY KEY REFERENCES prompt_templates(id) ON DELETE CASCADE,
    times_used INTEGER NOT NULL DEFAULT 0,
    total_reward REAL NOT NULL DEFAULT 0.0,
    avg_reward REAL NOT NULL DEFAULT 0.0,
    ucb_score REAL NOT NULL DEFAULT 9999.0,
    last_used_at INTEGER,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_library (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    source_session_id TEXT,
    source_template_id TEXT REFERENCES prompt_templates(id),
    task_type TEXT NOT NULL DEFAULT 'general',
    usage_count INTEGER NOT NULL DEFAULT 0,
    success_rate REAL NOT NULL DEFAULT 0.0,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_library_active ON skill_library(is_active, success_rate);
CREATE INDEX IF NOT EXISTS idx_skill_library_task ON skill_library(task_type, is_active);

-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS update_ucb_after_score
AFTER INSERT ON session_scores
WHEN NEW.template_id IS NOT NULL
BEGIN
    INSERT INTO prompt_ucb_stats (template_id, times_used, total_reward, avg_reward, ucb_score, updated_at)
    VALUES (NEW.template_id, 1, NEW.reward, NEW.reward, NEW.reward, unixepoch())
    ON CONFLICT(template_id) DO UPDATE SET
        times_used = times_used + 1,
        total_reward = total_reward + NEW.reward,
        avg_reward = (total_reward + NEW.reward) / (times_used + 1),
        ucb_score = (total_reward + NEW.reward) / (times_used + 1),
        updated_at = unixepoch();
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS update_ucb_after_score;
DROP INDEX IF EXISTS idx_skill_library_task;
DROP INDEX IF EXISTS idx_skill_library_active;
DROP TABLE IF EXISTS skill_library;
DROP TABLE IF EXISTS prompt_ucb_stats;
DROP INDEX IF EXISTS idx_session_scores_template;
DROP INDEX IF EXISTS idx_session_scores_session;
DROP TABLE IF EXISTS session_scores;
DROP INDEX IF EXISTS idx_prompt_templates_name;
DROP TABLE IF EXISTS prompt_templates;

-- +goose StatementEnd
