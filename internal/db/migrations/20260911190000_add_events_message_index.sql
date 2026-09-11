-- +goose Up
-- +goose StatementBegin

-- Mirrors idx_events_session (20260911000001_add_events_session_index.sql)
-- for the new per-message write/read paths added by the incremental session
-- indexer (EventStore.ReplaceMessageEvents / DeleteMessageEvents /
-- MessageEventMarkers in internal/rag/events/events.go): those queries filter
-- on `subject = ? AND json_extract(metadata, '$.message_id') = ?` (or, for
-- MessageEventMarkers, group by the same expression) instead of session_id.
-- Without this index, every per-message lookup would scan the whole table.
--
-- The indexed expression must stay byte-for-byte identical to the one used
-- in the query (same function, same JSON path literal) or SQLite will not
-- match this index against the query plan.
CREATE INDEX IF NOT EXISTS idx_events_message
    ON events(subject, json_extract(metadata, '$.message_id'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_events_message;

-- +goose StatementEnd
