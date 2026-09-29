-- +goose Up
-- +goose StatementBegin
-- Prompt/completion tokens of the LLM judge call for this session, so the daily
-- judge budget can be computed from persisted data. judge_analysis and
-- judge_model already exist. Updating these columns never touches reward, so
-- the UCB triggers (update_ucb_after_score / update_ucb_after_rescore) stay inert.
ALTER TABLE session_scores ADD COLUMN judge_prompt_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_scores ADD COLUMN judge_completion_tokens INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE session_scores DROP COLUMN judge_completion_tokens;
ALTER TABLE session_scores DROP COLUMN judge_prompt_tokens;
-- +goose StatementEnd
