-- +goose Up
ALTER TABLE sessions ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0);
ALTER TABLE sessions ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0);
ALTER TABLE sessions ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0);

-- +goose Down
ALTER TABLE sessions DROP COLUMN reasoning_tokens;
ALTER TABLE sessions DROP COLUMN cache_creation_tokens;
ALTER TABLE sessions DROP COLUMN cache_read_tokens;
