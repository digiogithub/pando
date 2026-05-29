-- +goose Up
ALTER TABLE sessions ADD COLUMN acp_session_state TEXT;

-- +goose Down
ALTER TABLE sessions DROP COLUMN acp_session_state;
