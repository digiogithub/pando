-- +goose Up
-- +goose StatementBegin
-- Learned skills are reviewable files (.pando/skills/learned/<id>.md); the
-- table mirrors their status and keeps the statistics. Rows that existed before
-- review have status 'legacy': they are deactivated (nothing unreviewed is
-- injected) and exported to pending files on the next startup sync.
ALTER TABLE skill_library ADD COLUMN status TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE skill_library ADD COLUMN confidence REAL NOT NULL DEFAULT 0.0;
ALTER TABLE skill_library ADD COLUMN judge_model TEXT NOT NULL DEFAULT '';
-- Evaluated sessions the skill was injected in and the sum of their rewards;
-- success_rate = reward_total / eval_count.
ALTER TABLE skill_library ADD COLUMN eval_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE skill_library ADD COLUMN reward_total REAL NOT NULL DEFAULT 0.0;
-- The old counters were bumped on every prompt build and never fed by rewards.
UPDATE skill_library SET is_active = 0, usage_count = 0, success_rate = 0.0;
-- +goose StatementEnd

-- +goose StatementBegin
-- Skills injected into each session's system prompt, chosen once on the first
-- prompt build and frozen so later turns and restarts serve identical bytes.
-- A row with an empty skill_id records "no skills" so a skill approved
-- mid-session is not picked up until the next session.
CREATE TABLE IF NOT EXISTS session_skill_injections (
    session_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    injected_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, skill_id)
);
CREATE INDEX IF NOT EXISTS idx_session_skill_injections_skill ON session_skill_injections(skill_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_session_skill_injections_skill;
DROP TABLE IF EXISTS session_skill_injections;
ALTER TABLE skill_library DROP COLUMN reward_total;
ALTER TABLE skill_library DROP COLUMN eval_count;
ALTER TABLE skill_library DROP COLUMN judge_model;
ALTER TABLE skill_library DROP COLUMN confidence;
ALTER TABLE skill_library DROP COLUMN status;
-- +goose StatementEnd
