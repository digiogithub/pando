// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/events"
)

// openTempEventStore opens a real, temp-file-backed events.EventStore with
// the events/events_fts schema (mirroring
// internal/db/migrations/20260311000002_add_events.sql plus the session and
// message expression indexes), so the dispatcher round trip below exercises
// the real EventStore write path, not a mock.
func openTempEventStore(t *testing.T) *events.EventStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	dsn := fmt.Sprintf("file:%s?_txlock=immediate", path)
	sqlDB, err := sqlite3driver.Open(dsn)
	if err != nil {
		t.Fatalf("open temp event store DB: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	schema := `
		CREATE TABLE IF NOT EXISTS events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			subject    TEXT    NOT NULL DEFAULT '',
			content    TEXT    NOT NULL,
			metadata   TEXT    NOT NULL DEFAULT '{}',
			embedding  BLOB,
			event_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		);
		CREATE INDEX IF NOT EXISTS idx_events_subject  ON events(subject);
		CREATE INDEX IF NOT EXISTS idx_events_session ON events(subject, json_extract(metadata,'$.session_id'));
		CREATE INDEX IF NOT EXISTS idx_events_message ON events(subject, json_extract(metadata,'$.message_id'));
		CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
			subject, content, content='events', content_rowid='id', tokenize='porter unicode61'
		);
	`
	if _, err := sqlDB.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return events.NewEventStore(sqlDB, nil)
}

// TestDispatchRemembrancesWrite_ReplaceMessageEventsRoundTrip proves the new
// write op's wire format round-trips correctly: a JSON payload shaped like
// events.replaceMessageEventsRequest, dispatched by method name through
// RemembrancesWriteDispatcher exactly as a secondary's forwarded IPC write
// would arrive on the primary, ends up calling
// EventStore.ReplaceMessageEvents and the row is actually written.
func TestDispatchRemembrancesWrite_ReplaceMessageEventsRoundTrip(t *testing.T) {
	store := openTempEventStore(t)
	svc := &rag.RemembrancesService{Events: store}
	d := NewRemembrancesWriteDispatcher(svc)

	params, err := json.Marshal(replaceMessageEventsReq{
		SessionID:  "session-1",
		MessageID:  "msg-1",
		Subject:    "session",
		Metadata:   map[string]interface{}{"session_id": "session-1", "message_id": "msg-1", "content_hash": "h1"},
		Chunks:     []string{"round trip chunk"},
		Embeddings: [][]float32{{1, 1}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	ctx := context.Background()
	if _, err := d.DispatchRemembrancesWrite(ctx, "ReplaceMessageEvents", params); err != nil {
		t.Fatalf("DispatchRemembrancesWrite(ReplaceMessageEvents) error = %v", err)
	}

	markers, err := store.MessageEventMarkers(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}
	if markers["msg-1"] != "h1" {
		t.Fatalf("expected msg-1 marker %q after dispatch, got markers = %v", "h1", markers)
	}
}

// TestDispatchRemembrancesWrite_DeleteMessageEventsRoundTrip mirrors the
// above for the delete op.
func TestDispatchRemembrancesWrite_DeleteMessageEventsRoundTrip(t *testing.T) {
	store := openTempEventStore(t)
	svc := &rag.RemembrancesService{Events: store}
	d := NewRemembrancesWriteDispatcher(svc)
	ctx := context.Background()

	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session",
		map[string]interface{}{"session_id": "session-1", "message_id": "msg-1"},
		[]string{"to be deleted"}, [][]float32{{1, 1}},
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	params, err := json.Marshal(deleteMessageEventsReq{MessageID: "msg-1", Subject: "session"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	if _, err := d.DispatchRemembrancesWrite(ctx, "DeleteMessageEvents", params); err != nil {
		t.Fatalf("DispatchRemembrancesWrite(DeleteMessageEvents) error = %v", err)
	}

	markers, err := store.MessageEventMarkers(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}
	if _, ok := markers["msg-1"]; ok {
		t.Fatalf("expected msg-1 to have no marker after dispatched delete, got markers = %v", markers)
	}
}

// TestDispatchRemembrancesWrite_UnsupportedMethodMessageMatchesVersionSkewDetection
// pins the exact error text the dispatcher's default case produces for an
// unrecognised method name — the shape a secondary sees (after the message
// crosses the IPC boundary as plain text) when it talks to an older primary
// binary that predates a given write op. This is the literal string
// internal/ipc/dbproxy.mapToWriteError matches to classify the failure as
// ErrCodeMethodNotFound (see that package's errors_test.go for the
// corresponding assertion on the consumer side), so this test guards that
// the two sides of that contract — the message dispatcher.go actually
// produces, and the message dbproxy actually recognises — do not drift
// apart.
func TestDispatchRemembrancesWrite_UnsupportedMethodMessageMatchesVersionSkewDetection(t *testing.T) {
	store := openTempEventStore(t)
	svc := &rag.RemembrancesService{Events: store}
	d := NewRemembrancesWriteDispatcher(svc)

	_, err := d.DispatchRemembrancesWrite(context.Background(), "SomeFutureMethodThisPrimaryPredates", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected an error for an unrecognised method")
	}
	if !strings.Contains(err.Error(), "unsupported remembrances write method") {
		t.Fatalf("dispatcher error = %q, want it to contain %q (the text dbproxy.mapToWriteError matches for version skew)", err.Error(), "unsupported remembrances write method")
	}
}
