package events

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func openTestEventStoreDB(t *testing.T) *sql.DB {
	t.Helper()
	dbConn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	migration := `
	CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		subject TEXT NOT NULL,
		content TEXT NOT NULL,
		metadata TEXT NOT NULL DEFAULT '{}',
		embedding BLOB,
		event_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL
	);
	CREATE INDEX idx_events_subject ON events(subject);
	CREATE INDEX idx_events_event_at ON events(event_at);
	CREATE VIRTUAL TABLE events_fts USING fts5(
		subject,
		content,
		content='events',
		content_rowid='id'
	);
	`
	if _, err := dbConn.Exec(migration); err != nil {
		t.Fatalf("db.Exec(migration) error = %v", err)
	}
	return dbConn
}

func TestReplaceSessionEventsReplacesExistingChunks(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, nil)
	ctx := context.Background()

	firstMetadata := map[string]interface{}{
		"session_id":    "session-1",
		"title":         "First",
		"message_count": 1,
		"source":        "pando_session",
		"updated_at":    time.Now().Unix(),
	}
	if err := store.ReplaceSessionEvents(ctx, "session-1", "session", firstMetadata, []string{"chunk one", "chunk two"}, [][]float32{{1, 1}, {2, 2}}); err != nil {
		t.Fatalf("first ReplaceSessionEvents() error = %v", err)
	}

	secondMetadata := map[string]interface{}{
		"session_id":    "session-1",
		"title":         "Second",
		"message_count": 2,
		"source":        "pando_session",
		"updated_at":    time.Now().Unix(),
	}
	if err := store.ReplaceSessionEvents(ctx, "session-1", "session", secondMetadata, []string{"fresh chunk"}, [][]float32{{3, 3}}); err != nil {
		t.Fatalf("second ReplaceSessionEvents() error = %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT content, metadata FROM events ORDER BY id`)
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}
	defer rows.Close()

	count := 0
	var content, metadataJSON string
	for rows.Next() {
		count++
		if err := rows.Scan(&content, &metadataJSON); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error = %v", err)
	}

	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
	if content != "fresh chunk" {
		t.Fatalf("content = %q, want %q", content, "fresh chunk")
	}
	if metadataJSON == "" || metadataJSON == "{}" {
		t.Fatalf("metadata should be populated, got %q", metadataJSON)
	}

	var ftsCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("fts count query error = %v", err)
	}
	if ftsCount != 1 {
		t.Fatalf("fts count = %d, want 1", ftsCount)
	}
}

func TestReplaceSessionEventsRejectsMismatchedEmbeddings(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, nil)

	err := store.ReplaceSessionEvents(context.Background(), "session-1", "session", map[string]interface{}{"session_id": "session-1"}, []string{"chunk one", "chunk two"}, [][]float32{{1, 1}})
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
}
