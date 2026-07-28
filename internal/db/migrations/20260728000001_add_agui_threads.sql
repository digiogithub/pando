-- +goose Up
-- +goose StatementBegin

-- agui_threads maps AG-UI thread ids (owned by the browser client) to the Pando
-- session that backs them. The AG-UI adapter is a removable side-car, so the
-- mapping lives in its own table instead of adding a column to `sessions`:
-- dropping the feature is then a matter of dropping this table.
--
-- The session id is NOT a foreign key. A deleted session must not delete the
-- client's thread silently; the adapter validates the session on read and
-- rebinds the thread to a fresh one when it is gone.
CREATE TABLE IF NOT EXISTS agui_threads (
    thread_id  TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    agent      TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_agui_threads_session ON agui_threads(session_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_agui_threads_session;
DROP TABLE IF EXISTS agui_threads;

-- +goose StatementEnd
