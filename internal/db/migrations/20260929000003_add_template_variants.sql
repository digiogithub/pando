-- +goose Up
-- +goose StatementBegin
-- Prompt template variants are now human-authored files
-- (.pando/prompts/variants/<section>/<variant>.md.tpl); the database only holds
-- statistics. The old seeded prompt_templates rows (rendered with nil data, keyed
-- by a section name the builder never used) and their per-template UCB stats are
-- stale and would never match a variant id, so they are removed. The tables stay
-- (empty) so older binaries keep starting.
UPDATE session_scores SET template_id = NULL WHERE template_id IS NOT NULL;
UPDATE skill_library SET source_template_id = NULL WHERE source_template_id IS NOT NULL;
DELETE FROM prompt_ucb_stats;
DELETE FROM prompt_templates;
-- +goose StatementEnd

-- +goose StatementBegin
-- UCB aggregates are maintained from Go (one statement per evaluation, joined to
-- session_template_selections), keyed by variant id. The triggers below wrote
-- per-template rows from session_scores.template_id and would now be wrong.
DROP TRIGGER IF EXISTS update_ucb_after_score;
DROP TRIGGER IF EXISTS update_ucb_after_rescore;
-- +goose StatementEnd

-- +goose StatementBegin
-- Variant chosen for each prompt section of a session. Written once per
-- (session, section) and frozen, so restarts keep the same prompt bytes and the
-- evaluation can attribute the reward to the right variants.
CREATE TABLE IF NOT EXISTS session_template_selections (
    session_id TEXT NOT NULL,
    section TEXT NOT NULL,
    variant_id TEXT NOT NULL,
    selected_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, section)
);
CREATE INDEX IF NOT EXISTS idx_session_template_selections_variant ON session_template_selections(section, variant_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Per-variant statistics. variant_id is "<section>#<variant>" ("<section>#default"
-- is the embedded template).
CREATE TABLE IF NOT EXISTS prompt_variant_stats (
    variant_id TEXT PRIMARY KEY,
    section TEXT NOT NULL,
    times_used INTEGER NOT NULL DEFAULT 0,
    total_reward REAL NOT NULL DEFAULT 0.0,
    avg_reward REAL NOT NULL DEFAULT 0.0,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_prompt_variant_stats_section ON prompt_variant_stats(section);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_prompt_variant_stats_section;
DROP TABLE IF EXISTS prompt_variant_stats;
DROP INDEX IF EXISTS idx_session_template_selections_variant;
DROP TABLE IF EXISTS session_template_selections;
-- +goose StatementEnd
