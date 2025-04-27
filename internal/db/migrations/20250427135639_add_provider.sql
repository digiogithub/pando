-- +goose Up
-- +goose StatementBegin
ALTER TABLE messages ADD COLUMN provider TEXT DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE messages DROP COLUMN provider;
-- +goose StatementEnd
