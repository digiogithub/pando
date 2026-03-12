-- +goose Up
-- +goose StatementBegin

-- events stores temporal events with semantic search capabilities.
-- Each event has subject, content, optional metadata, and an embedding vector.
CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    subject    TEXT    NOT NULL DEFAULT '',
    content    TEXT    NOT NULL,
    metadata   TEXT    NOT NULL DEFAULT '{}',
    -- embedding is a little-endian blob of float32 values (4 bytes each).
    -- NULL means no embedding has been stored for this event.
    embedding  BLOB,
    event_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_events_subject  ON events(subject);
CREATE INDEX IF NOT EXISTS idx_events_event_at ON events(event_at);

-- events_fts is a FTS5 external-content virtual table backed by events.
-- The application must keep this index in sync on insert/delete.
CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
    subject,
    content,
    content       = 'events',
    content_rowid = 'id',
    tokenize      = 'porter unicode61'
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS events_fts;
DROP INDEX IF EXISTS idx_events_event_at;
DROP INDEX IF EXISTS idx_events_subject;
DROP TABLE IF EXISTS events;

-- +goose StatementEnd
