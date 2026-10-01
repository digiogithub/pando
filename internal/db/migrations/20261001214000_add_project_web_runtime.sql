-- +goose Up
ALTER TABLE projects ADD COLUMN web_pid INTEGER;
ALTER TABLE projects ADD COLUMN web_port INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE projects DROP COLUMN web_port;
ALTER TABLE projects DROP COLUMN web_pid;
