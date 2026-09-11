-- +goose Up
-- +goose StatementBegin

-- The session indexer's read-then-write replace (EventStore.ReplaceSessionEvents
-- / deleteSessionEventsTx in internal/rag/events/events.go) selects and deletes
-- every row for one session with
-- `WHERE subject = ? AND json_extract(metadata, '$.session_id') = ?`.
-- Only idx_events_subject(subject) existed, and virtually every row has
-- subject='session' in practice, so that predicate was effectively a full
-- table scan plus a json_extract() call per row. This expression index lets
-- SQLite match the exact (subject, json_extract(...)) pair directly,
-- shrinking the read — and the write-lock hold time now that write
-- transactions use _txlock=immediate, see internal/db/connect.go — from
-- O(events) to roughly O(rows in that session).
--
-- The indexed expression must stay byte-for-byte identical to the one used
-- in the query (same function, same JSON path literal) or SQLite will not
-- match this index against the query plan.
CREATE INDEX IF NOT EXISTS idx_events_session
    ON events(subject, json_extract(metadata, '$.session_id'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_events_session;

-- +goose StatementEnd
