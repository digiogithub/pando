-- +goose Up
-- +goose StatementBegin
-- components holds the JSON decomposition of the reward (per-signal scores,
-- weights, correction pattern hits, explicit feedback) so a UI can explain it.
ALTER TABLE session_scores ADD COLUMN components TEXT NOT NULL DEFAULT '{}';
-- +goose StatementEnd

-- +goose StatementBegin
-- A re-score (e.g. after explicit /feedback) UPDATEs the existing row instead of
-- inserting a second one, so the insert trigger update_ucb_after_score does not
-- fire. This trigger applies only the reward delta to the UCB aggregates and
-- leaves times_used untouched, so a session is never counted twice.
CREATE TRIGGER IF NOT EXISTS update_ucb_after_rescore
AFTER UPDATE OF reward ON session_scores
WHEN NEW.template_id IS NOT NULL AND NEW.reward != OLD.reward
BEGIN
    UPDATE prompt_ucb_stats SET
        total_reward = total_reward + NEW.reward - OLD.reward,
        avg_reward = (total_reward + NEW.reward - OLD.reward) / MAX(times_used, 1),
        ucb_score = (total_reward + NEW.reward - OLD.reward) / MAX(times_used, 1),
        updated_at = unixepoch()
    WHERE template_id = NEW.template_id;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_ucb_after_rescore;
ALTER TABLE session_scores DROP COLUMN components;
-- +goose StatementEnd
