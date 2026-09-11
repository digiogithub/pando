// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package events

import (
	"context"
	"strings"
	"testing"
)

// fakeQueryEmbedder satisfies embeddings.Embedder with fixed, cheap vectors,
// just enough for EventStore.SearchEvents' hybrid (vector + FTS) query path
// to run without a real embedding provider. The tests in this file only
// assert on FTS-findability and exact row content, never on vector
// similarity ranking, so the actual vector values are unimportant.
type fakeQueryEmbedder struct{}

func (fakeQueryEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 1}
	}
	return out, nil
}

func (fakeQueryEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return []float32{1, 1}, nil
}

func (fakeQueryEmbedder) Dimension() int { return 2 }

// TestReplaceMessageEventsInsertsAndKeepsFTSInSync covers a fresh message: no
// existing rows, ReplaceMessageEvents inserts its chunks, and each chunk is
// searchable via FTS.
func TestReplaceMessageEventsInsertsAndKeepsFTSInSync(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	ctx := context.Background()

	metadata := map[string]interface{}{
		"session_id":   "session-1",
		"message_id":   "msg-1",
		"content_hash": "hash-a",
	}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", metadata, []string{"alpha content"}, [][]float32{{1, 1}}); err != nil {
		t.Fatalf("ReplaceMessageEvents() error = %v", err)
	}

	results, err := store.SearchEvents(ctx, SearchOptions{Query: "alpha", Subject: "session"})
	if err != nil {
		t.Fatalf("SearchEvents() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS to find the newly indexed message content")
	}
}

// TestReplaceMessageEventsReplacesOnlyThatMessage seeds two messages in the
// same session, replaces one of them, and asserts: the replaced message's
// old chunk is gone (FTS no longer finds it) and its new chunk is
// searchable, while the other message's row is completely untouched.
func TestReplaceMessageEventsReplacesOnlyThatMessage(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	ctx := context.Background()

	otherMeta := map[string]interface{}{"session_id": "session-1", "message_id": "msg-other", "content_hash": "h-other"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-other", "session", otherMeta, []string{"untouched sentinel content"}, [][]float32{{9, 9}}); err != nil {
		t.Fatalf("seed other message ReplaceMessageEvents() error = %v", err)
	}

	firstMeta := map[string]interface{}{"session_id": "session-1", "message_id": "msg-1", "content_hash": "hash-a"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", firstMeta, []string{"old chunk one", "old chunk two"}, [][]float32{{1, 1}, {2, 2}}); err != nil {
		t.Fatalf("first ReplaceMessageEvents() error = %v", err)
	}

	secondMeta := map[string]interface{}{"session_id": "session-1", "message_id": "msg-1", "content_hash": "hash-b"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", secondMeta, []string{"fresh replacement chunk"}, [][]float32{{3, 3}}); err != nil {
		t.Fatalf("second ReplaceMessageEvents() error = %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT content FROM events WHERE json_extract(metadata,'$.message_id') = 'msg-1' ORDER BY id`)
	if err != nil {
		t.Fatalf("query rows for msg-1: %v", err)
	}
	defer rows.Close()
	var contents []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		contents = append(contents, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err(): %v", err)
	}
	if len(contents) != 1 || contents[0] != "fresh replacement chunk" {
		t.Fatalf("msg-1 rows = %v, want exactly [\"fresh replacement chunk\"]", contents)
	}

	// The other message's row must be completely untouched.
	otherResults, err := store.SearchEvents(ctx, SearchOptions{Query: "sentinel", Subject: "session"})
	if err != nil {
		t.Fatalf("SearchEvents(sentinel) error = %v", err)
	}
	if len(otherResults) == 0 {
		t.Fatal("expected the untouched other message to still be searchable")
	}

	// The old chunk's text must no longer be found via FTS (proves the FTS
	// external-content index was kept in sync on delete, not just the events
	// table).
	oldResults, err := store.SearchEvents(ctx, SearchOptions{Query: "old chunk", Subject: "session"})
	if err != nil {
		t.Fatalf("SearchEvents(old chunk) error = %v", err)
	}
	for _, r := range oldResults {
		if strings.Contains(r.Event.Content, "old chunk") {
			t.Fatalf("expected the replaced message's old FTS entry to be gone, still found: %q", r.Event.Content)
		}
	}

	// The new chunk must be found.
	freshResults, err := store.SearchEvents(ctx, SearchOptions{Query: "fresh replacement", Subject: "session"})
	if err != nil {
		t.Fatalf("SearchEvents(fresh replacement) error = %v", err)
	}
	if len(freshResults) == 0 {
		t.Fatal("expected the new chunk to be searchable via FTS")
	}
}

// TestDeleteMessageEventsRemovesOnlyThatMessage mirrors the replace test but
// for deletion: deleting one message's rows must not touch another
// message's rows in the same session, and the deleted content must no
// longer be found by FTS.
func TestDeleteMessageEventsRemovesOnlyThatMessage(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	ctx := context.Background()

	keepMeta := map[string]interface{}{"session_id": "session-1", "message_id": "msg-keep", "content_hash": "h-keep"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-keep", "session", keepMeta, []string{"keep me searchable"}, [][]float32{{1, 1}}); err != nil {
		t.Fatalf("seed keep message: %v", err)
	}
	goneMeta := map[string]interface{}{"session_id": "session-1", "message_id": "msg-gone", "content_hash": "h-gone"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-gone", "session", goneMeta, []string{"delete me please"}, [][]float32{{2, 2}}); err != nil {
		t.Fatalf("seed gone message: %v", err)
	}

	if err := store.DeleteMessageEvents(ctx, "msg-gone", "session"); err != nil {
		t.Fatalf("DeleteMessageEvents() error = %v", err)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE json_extract(metadata,'$.message_id') = 'msg-gone'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected 0 rows left for msg-gone, got %d", remaining)
	}

	var ftsCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("fts count: %v", err)
	}
	if ftsCount != 1 {
		t.Fatalf("expected exactly 1 FTS row left (the kept message), got %d", ftsCount)
	}

	keepResults, err := store.SearchEvents(ctx, SearchOptions{Query: "keep me searchable", Subject: "session"})
	if err != nil {
		t.Fatalf("SearchEvents(keep) error = %v", err)
	}
	if len(keepResults) == 0 {
		t.Fatal("expected the kept message to still be searchable")
	}
}

// TestDeleteMessageEventsNoopWhenNothingIndexed proves deleting a message
// with no indexed rows is a no-op, not an error.
func TestDeleteMessageEventsNoopWhenNothingIndexed(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	if err := store.DeleteMessageEvents(context.Background(), "never-indexed", "session"); err != nil {
		t.Fatalf("DeleteMessageEvents() on a never-indexed message error = %v, want nil", err)
	}
}

// TestReplaceMessageEventsRejectsEmptyIdentifiers pins the input validation
// on the two identifiers ReplaceMessageEvents relies on for its delete scope
// and its metadata contract.
func TestReplaceMessageEventsRejectsEmptyIdentifiers(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	ctx := context.Background()

	if err := store.ReplaceMessageEvents(ctx, "", "msg-1", "session", nil, []string{"x"}, [][]float32{{1}}); err == nil {
		t.Fatal("expected error for empty session_id")
	}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "", "session", nil, []string{"x"}, [][]float32{{1}}); err == nil {
		t.Fatal("expected error for empty message_id")
	}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", nil, []string{"a", "b"}, [][]float32{{1}}); err == nil {
		t.Fatal("expected error for mismatched chunk/embedding counts")
	}
}

// TestSessionHasLegacyRows exercises the lazy-migration detector:
// false for a session with only per-message rows (or no rows at all), true
// once a legacy whole-transcript-style row (no message_id in its metadata)
// exists for that session.
func TestSessionHasLegacyRows(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	ctx := context.Background()

	has, err := store.SessionHasLegacyRows(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("SessionHasLegacyRows() error = %v", err)
	}
	if has {
		t.Fatal("expected no legacy rows for a session with nothing indexed yet")
	}

	msgMeta := map[string]interface{}{"session_id": "session-1", "message_id": "msg-1", "content_hash": "h"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", msgMeta, []string{"per-message chunk"}, [][]float32{{1, 1}}); err != nil {
		t.Fatalf("seed per-message row: %v", err)
	}
	has, err = store.SessionHasLegacyRows(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("SessionHasLegacyRows() error = %v", err)
	}
	if has {
		t.Fatal("expected no legacy rows for a session with only per-message rows")
	}

	// Simulate a pre-#6 whole-transcript row: metadata has session_id but no
	// message_id key at all.
	if err := store.ReplaceSessionEvents(ctx, "session-2", "session", map[string]interface{}{"session_id": "session-2"}, []string{"legacy whole transcript"}, [][]float32{{1, 1}}); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	has, err = store.SessionHasLegacyRows(ctx, "session-2", "session")
	if err != nil {
		t.Fatalf("SessionHasLegacyRows() error = %v", err)
	}
	if !has {
		t.Fatal("expected legacy rows to be detected for session-2")
	}

	// Unrelated session is unaffected.
	has, err = store.SessionHasLegacyRows(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("SessionHasLegacyRows() error = %v", err)
	}
	if has {
		t.Fatal("expected session-1 to remain free of legacy rows")
	}
}

// TestMessageEventMarkers proves the marker read returns exactly one
// message_id -> content_hash entry per indexed message, is scoped to the
// requested session, and picks up hash changes after a replace.
func TestMessageEventMarkers(t *testing.T) {
	db := openTestEventStoreDB(t)
	store := NewEventStore(db, fakeQueryEmbedder{})
	ctx := context.Background()

	meta1 := map[string]interface{}{"session_id": "session-1", "message_id": "msg-1", "content_hash": "hash-1"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", meta1, []string{"chunk a", "chunk b"}, [][]float32{{1, 1}, {2, 2}}); err != nil {
		t.Fatalf("seed msg-1: %v", err)
	}
	meta2 := map[string]interface{}{"session_id": "session-1", "message_id": "msg-2", "content_hash": "hash-2"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-2", "session", meta2, []string{"chunk c"}, [][]float32{{3, 3}}); err != nil {
		t.Fatalf("seed msg-2: %v", err)
	}
	// A different session must not leak into session-1's markers.
	otherMeta := map[string]interface{}{"session_id": "session-other", "message_id": "msg-3", "content_hash": "hash-3"}
	if err := store.ReplaceMessageEvents(ctx, "session-other", "msg-3", "session", otherMeta, []string{"other session chunk"}, [][]float32{{4, 4}}); err != nil {
		t.Fatalf("seed msg-3: %v", err)
	}

	markers, err := store.MessageEventMarkers(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}
	if len(markers) != 2 {
		t.Fatalf("markers = %v, want exactly 2 entries", markers)
	}
	if markers["msg-1"] != "hash-1" {
		t.Fatalf("markers[msg-1] = %q, want %q", markers["msg-1"], "hash-1")
	}
	if markers["msg-2"] != "hash-2" {
		t.Fatalf("markers[msg-2] = %q, want %q", markers["msg-2"], "hash-2")
	}
	if _, ok := markers["msg-3"]; ok {
		t.Fatal("expected msg-3 (a different session) not to appear in session-1's markers")
	}

	// Replacing msg-1 with a new hash must be reflected on the next read.
	meta1Updated := map[string]interface{}{"session_id": "session-1", "message_id": "msg-1", "content_hash": "hash-1-updated"}
	if err := store.ReplaceMessageEvents(ctx, "session-1", "msg-1", "session", meta1Updated, []string{"chunk a updated"}, [][]float32{{5, 5}}); err != nil {
		t.Fatalf("update msg-1: %v", err)
	}
	markers, err = store.MessageEventMarkers(ctx, "session-1", "session")
	if err != nil {
		t.Fatalf("MessageEventMarkers() (after update) error = %v", err)
	}
	if markers["msg-1"] != "hash-1-updated" {
		t.Fatalf("markers[msg-1] after update = %q, want %q", markers["msg-1"], "hash-1-updated")
	}
}

// TestMessageEventsQueryUsesExpressionIndex asserts that the exact query
// deleteMessageEventsTx runs is matched by idx_events_message — the
// migration and the query must keep using the identical expression (same
// function, same JSON path literal) for SQLite to pick the index, exactly
// like idx_events_session does for the session-scoped query (see
// TestSessionEventsQueryUsesExpressionIndex in session_index_test.go).
func TestMessageEventsQueryUsesExpressionIndex(t *testing.T) {
	db := openTestEventStoreDB(t)
	if _, err := db.Exec(`CREATE INDEX idx_events_message ON events(subject, json_extract(metadata, '$.message_id'));`); err != nil {
		t.Fatalf("create idx_events_message: %v", err)
	}

	rows, err := db.QueryContext(context.Background(), `
		EXPLAIN QUERY PLAN
		SELECT id, subject, content
		FROM events
		WHERE subject = ? AND json_extract(metadata, '$.message_id') = ?`,
		"session", "any-message",
	)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}

	if !strings.Contains(plan.String(), "idx_events_message") {
		t.Fatalf("expected query plan to use idx_events_message, got:\n%s", plan.String())
	}
}
