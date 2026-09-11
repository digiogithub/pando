// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/message"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/embeddings"
	"github.com/digiogithub/pando/internal/rag/events"
	"github.com/digiogithub/pando/internal/session"
)

func TestReplaceSessionEventsWithRetry_SucceedsAfterTransientBusy(t *testing.T) {
	calls := 0
	err := replaceSessionEventsWithRetry(context.Background(), "sess-1", func() error {
		calls++
		if calls < 3 {
			return &dbproxy.WriteError{Code: dbproxy.ErrCodeBusy, Message: "database is locked"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 failures + 1 success), got %d", calls)
	}
}

func TestReplaceSessionEventsWithRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	busyErr := &dbproxy.WriteError{Code: dbproxy.ErrCodeBusy, Message: "database is locked"}
	err := replaceSessionEventsWithRetry(context.Background(), "sess-1", func() error {
		calls++
		return busyErr
	})
	wantCalls := 1 + sessionIndexReplaceRetries
	if calls != wantCalls {
		t.Fatalf("expected %d calls (initial + %d retries), got %d", wantCalls, sessionIndexReplaceRetries, calls)
	}
	if !errors.Is(err, busyErr) && err != busyErr {
		t.Fatalf("expected the last busy error to be returned, got %v", err)
	}
}

func TestReplaceSessionEventsWithRetry_NonRetryableErrorReturnsImmediately(t *testing.T) {
	calls := 0
	permanent := errors.New("boom: constraint violated")
	err := replaceSessionEventsWithRetry(context.Background(), "sess-1", func() error {
		calls++
		return permanent
	})
	if calls != 1 {
		t.Fatalf("expected exactly 1 call for a non-retryable error, got %d", calls)
	}
	if !errors.Is(err, permanent) {
		t.Fatalf("expected the permanent error to be returned as-is, got %v", err)
	}
}

func TestReplaceSessionEventsWithRetry_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the first retry backoff

	calls := 0
	busyErr := &dbproxy.WriteError{Code: dbproxy.ErrCodeBusy, Message: "database is locked"}
	start := time.Now()
	err := replaceSessionEventsWithRetry(ctx, "sess-1", func() error {
		calls++
		return busyErr
	})
	elapsed := time.Since(start)

	if calls != 1 {
		t.Fatalf("expected exactly 1 call before the canceled ctx aborts the retry loop, got %d", calls)
	}
	if err == nil {
		t.Fatal("expected an error to be returned")
	}
	// sessionIndexReplaceBaseBackoff is 250ms; a canceled ctx must not sleep
	// through it.
	if elapsed > 100*time.Millisecond {
		t.Fatalf("replaceSessionEventsWithRetry took %s, want it to return promptly on a canceled ctx", elapsed)
	}
}

// openTempEventStore creates a real, temp-file-backed events.EventStore with
// exactly the events/events_fts schema from
// internal/db/migrations/20260311000002_add_events.sql, opened with the same
// "_txlock=immediate" DSN parameter the production pool uses (see
// [[pando/fixes/sqlite_immediate_tx_conn_pragmas_session_index.md]]) so a
// concurrent writer produces a genuine SQLITE_BUSY instead of the old silent
// read->write upgrade.
func openTempEventStore(t *testing.T, embedder embeddings.Embedder) (*events.EventStore, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	// A short busy_timeout (20ms) so a lock collision fails fast with a real
	// SQLITE_BUSY instead of SQLite's own busy handler silently absorbing the
	// contention by blocking — the retry loop under test must be the thing
	// that waits out the conflict, not the driver.
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=busy_timeout(20)", path)
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
		CREATE INDEX IF NOT EXISTS idx_events_event_at ON events(event_at);
		CREATE INDEX IF NOT EXISTS idx_events_session ON events(subject, json_extract(metadata,'$.session_id'));
		CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
			subject, content, content='events', content_rowid='id', tokenize='porter unicode61'
		);
	`
	if _, err := sqlDB.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return events.NewEventStore(sqlDB, embedder), sqlDB
}

// TestIndexSessionConversationRetriesRealBusyErrorAndReusesEmbeddings drives
// indexSessionConversation end-to-end against a real temp SQLite DB: a
// competing BEGIN IMMEDIATE transaction holds the write lock just long
// enough that the first ReplaceSessionEvents attempt gets a genuine
// SQLITE_BUSY, then releases it before the retries are exhausted. This
// proves the retry wiring in indexSessionConversation (not just the isolated
// replaceSessionEventsWithRetry helper) recovers from a real lock conflict,
// and that EmbedDocuments — the expensive, non-idempotent step — is called
// exactly once despite the retried write.
func TestIndexSessionConversationRetriesRealBusyErrorAndReusesEmbeddings(t *testing.T) {
	embedder := &recordingEmbedder{}
	store, sqlDB := openTempEventStore(t, embedder)

	app := &App{
		Sessions: &indexingSessionService{sess: session.Session{ID: "session-1", Title: "Busy retry"}},
		Messages: &indexingMessagesService{msgs: []message.Message{{
			SessionID: "session-1",
			Role:      message.User,
			Parts:     []message.ContentPart{message.TextContent{Text: "hello"}},
		}}},
	}
	svc := &rag.RemembrancesService{Events: store}
	setDocumentEmbedderForTest(svc, embedder)

	// Hold the write lock on a second connection just long enough (150ms) to
	// force a genuine BUSY on the first attempt (busy_timeout is only 20ms),
	// but well under the first retry's 250ms backoff — so the retry that
	// fires after that backoff finds the lock already released and
	// succeeds.
	blockerTx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin blocking tx: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		blockerTx.Rollback() //nolint:errcheck
		close(released)
	}()
	defer func() { <-released }()

	if err := app.indexSessionConversation(context.Background(), svc, "session-1"); err != nil {
		t.Fatalf("indexSessionConversation should have recovered after retrying, got %v", err)
	}

	if embedder.callCount != 1 {
		t.Fatalf("expected EmbedDocuments to be called exactly once (embeddings reused across retries), got %d", embedder.callCount)
	}

	count, err := store.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count == 0 {
		t.Fatal("expected the retried ReplaceSessionEvents to have actually written events")
	}
}
