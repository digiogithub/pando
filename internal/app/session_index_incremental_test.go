// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package app

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/session"
)

// newIncrementalTestApp wires an App with a real temp-file EventStore (so
// SessionHasLegacyRows/MessageEventMarkers/ReplaceMessageEvents/
// DeleteMessageEvents all run against real SQL, not a mock) plus the
// existing indexingSessionService/indexingMessagesService test fakes
// (remembrances_indexer_test.go) and a recordingEmbedder (also defined
// there) so each run's embed call count is observable.
func newIncrementalTestApp(t *testing.T, sess session.Session, msgs []message.Message) (*App, *rag.RemembrancesService, *recordingEmbedder, *indexingSessionService, *indexingMessagesService) {
	t.Helper()
	embedder := &recordingEmbedder{}
	store, _ := openTempEventStore(t, embedder)
	sessSvc := &indexingSessionService{sess: sess}
	msgSvc := &indexingMessagesService{msgs: msgs}
	app := &App{Sessions: sessSvc, Messages: msgSvc}
	svc := &rag.RemembrancesService{Events: store}
	setDocumentEmbedderForTest(svc, embedder)
	return app, svc, embedder, sessSvc, msgSvc
}

// TestIndexSessionConversation_UnchangedMessagesNotReembedded proves the
// core incremental-indexing win: once a session has been indexed, running
// indexSessionConversation again with nothing changed must not call
// EmbedDocuments at all (every message's — and the header's — content hash
// still matches what MessageEventMarkers already has stored).
func TestIndexSessionConversation_UnchangedMessagesNotReembedded(t *testing.T) {
	msgs := []message.Message{
		{ID: "msg-1", SessionID: "session-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}},
		{ID: "msg-2", SessionID: "session-1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "hi there"}}},
	}
	app, svc, embedder, _, _ := newIncrementalTestApp(t, session.Session{ID: "session-1", Title: "Greeting"}, msgs)
	ctx := context.Background()

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("first indexSessionConversation() error = %v", err)
	}
	firstRunCalls := embedder.callCount
	if firstRunCalls == 0 {
		t.Fatal("expected the first run to embed at least the header and the two messages")
	}

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("second indexSessionConversation() error = %v", err)
	}
	if embedder.callCount != firstRunCalls {
		t.Fatalf("expected no additional EmbedDocuments calls on an unchanged re-run, got %d more (total %d)",
			embedder.callCount-firstRunCalls, embedder.callCount)
	}
}

// TestIndexSessionConversation_ChangedMessageReembeddedAlone proves that
// when exactly one message's content changes between runs, only that
// message is re-embedded and rewritten — the header and the other message
// are left untouched.
func TestIndexSessionConversation_ChangedMessageReembeddedAlone(t *testing.T) {
	msgs := []message.Message{
		{ID: "msg-1", SessionID: "session-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "original text"}}},
		{ID: "msg-2", SessionID: "session-1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "reply text"}}},
	}
	app, svc, embedder, _, msgSvc := newIncrementalTestApp(t, session.Session{ID: "session-1", Title: "Edit test"}, msgs)
	ctx := context.Background()

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("first indexSessionConversation() error = %v", err)
	}

	markersBefore, err := svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}

	// Change only msg-1's content; msg-2 and the title stay identical.
	msgSvc.msgs = []message.Message{
		{ID: "msg-1", SessionID: "session-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "edited text"}}},
		msgs[1],
	}

	embedder.callCount = 0
	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("second indexSessionConversation() error = %v", err)
	}
	if embedder.callCount != 1 {
		t.Fatalf("expected exactly 1 EmbedDocuments call (only the changed message), got %d", embedder.callCount)
	}
	if len(embedder.texts) != 1 || embedder.texts[0] != "Session: Edit test\nUSER:\nedited text" {
		t.Fatalf("expected the re-embedded content to be the changed message's new content, got %#v", embedder.texts)
	}

	markersAfter, err := svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() (after) error = %v", err)
	}
	if markersAfter["msg-1"] == markersBefore["msg-1"] {
		t.Fatal("expected msg-1's marker hash to change after its content changed")
	}
	if markersAfter["msg-2"] != markersBefore["msg-2"] {
		t.Fatal("expected msg-2's marker hash to stay the same (it was not touched)")
	}
	if markersAfter[sessionHeaderMessageID] != markersBefore[sessionHeaderMessageID] {
		t.Fatal("expected the header row's marker hash to stay the same (the title did not change)")
	}
}

// TestIndexSessionConversation_DeletedMessageRowsRemoved proves that when a
// message that was previously indexed no longer appears in the session's
// current message list (e.g. history truncation), its indexed rows are
// deleted on the next run, while other messages' rows are left alone.
func TestIndexSessionConversation_DeletedMessageRowsRemoved(t *testing.T) {
	msgs := []message.Message{
		{ID: "msg-1", SessionID: "session-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "keep me"}}},
		{ID: "msg-2", SessionID: "session-1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "remove me"}}},
	}
	app, svc, _, _, msgSvc := newIncrementalTestApp(t, session.Session{ID: "session-1", Title: "Truncation test"}, msgs)
	ctx := context.Background()

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("first indexSessionConversation() error = %v", err)
	}
	markers, err := svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}
	if _, ok := markers["msg-2"]; !ok {
		t.Fatalf("expected msg-2 to be indexed after the first run, markers = %v", markers)
	}

	// msg-2 no longer exists in the session's message list.
	msgSvc.msgs = []message.Message{msgs[0]}

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("second indexSessionConversation() error = %v", err)
	}

	markers, err = svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() (after) error = %v", err)
	}
	if _, ok := markers["msg-2"]; ok {
		t.Fatalf("expected msg-2's rows to be removed once it no longer exists, markers = %v", markers)
	}
	if _, ok := markers["msg-1"]; !ok {
		t.Fatalf("expected msg-1 to remain indexed, markers = %v", markers)
	}
	if _, ok := markers[sessionHeaderMessageID]; !ok {
		t.Fatalf("expected the header row to remain indexed, markers = %v", markers)
	}
}

// TestIndexSessionConversation_LegacyRowsMigratedOnFirstRun seeds a
// pre-#6-shaped whole-transcript row (metadata with no "message_id" key —
// exactly what EventStore.ReplaceSessionEvents wrote before this feature
// existed) directly into the store, then proves the first incremental run
// for that session detects it, clears it, and writes normal per-message
// rows instead — with no leftover legacy row.
func TestIndexSessionConversation_LegacyRowsMigratedOnFirstRun(t *testing.T) {
	msgs := []message.Message{
		{ID: "msg-1", SessionID: "session-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "post-migration content"}}},
	}
	app, svc, _, _, _ := newIncrementalTestApp(t, session.Session{ID: "session-1", Title: "Migration test"}, msgs)
	ctx := context.Background()

	legacyMetadata := map[string]interface{}{"session_id": "session-1"} // no message_id key: legacy shape
	if err := svc.Events.ReplaceSessionEvents(ctx, "session-1", sessionIndexSubject, legacyMetadata, []string{"legacy whole transcript blob"}, [][]float32{{1, 1}}); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	hasLegacy, err := svc.Events.SessionHasLegacyRows(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("SessionHasLegacyRows() (before) error = %v", err)
	}
	if !hasLegacy {
		t.Fatal("expected the seeded row to be detected as legacy before the first incremental run")
	}

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("indexSessionConversation() error = %v", err)
	}

	hasLegacy, err = svc.Events.SessionHasLegacyRows(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("SessionHasLegacyRows() (after) error = %v", err)
	}
	if hasLegacy {
		t.Fatal("expected the legacy row to be cleared by the first incremental run")
	}

	markers, err := svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}
	if _, ok := markers["msg-1"]; !ok {
		t.Fatalf("expected msg-1 to be indexed as a normal per-message row after migration, markers = %v", markers)
	}
	if _, ok := markers[sessionHeaderMessageID]; !ok {
		t.Fatalf("expected the header row to be indexed after migration, markers = %v", markers)
	}

	// The old whole-transcript content must be gone entirely, not merely
	// superseded alongside new rows. ListEvents (a plain DB list, no query
	// embedding involved — recordingEmbedder.EmbedQuery deliberately errors,
	// since only EmbedDocuments should ever be called by the indexer) is
	// used here rather than SearchEvents to keep this assertion independent
	// of the embedder.
	all, err := svc.Events.ListEvents(ctx, sessionIndexSubject, 100, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, e := range all {
		if e.Content == "legacy whole transcript blob" {
			t.Fatal("expected the legacy row's content to be gone after migration")
		}
	}
}

// TestIndexSessionConversation_HeaderRowUpdatedOnTitleChange proves the
// documented trade-off of messageIndexContent/sessionHeaderIndexContent: a
// session-title-only change updates just the header row's hash (and
// content), leaving every message's own marker hash untouched — so a title
// edit does not force a full re-embed of the whole session.
func TestIndexSessionConversation_HeaderRowUpdatedOnTitleChange(t *testing.T) {
	msgs := []message.Message{
		{ID: "msg-1", SessionID: "session-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "unrelated to the title"}}},
	}
	app, svc, embedder, sessSvc, _ := newIncrementalTestApp(t, session.Session{ID: "session-1", Title: "Old Title"}, msgs)
	ctx := context.Background()

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("first indexSessionConversation() error = %v", err)
	}
	markersBefore, err := svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() error = %v", err)
	}

	sessSvc.sess.Title = "New Title"
	embedder.callCount = 0

	if err := app.indexSessionConversation(ctx, svc, "session-1"); err != nil {
		t.Fatalf("second indexSessionConversation() error = %v", err)
	}
	if embedder.callCount != 1 {
		t.Fatalf("expected exactly 1 EmbedDocuments call (the header row only) after a title-only change, got %d", embedder.callCount)
	}

	markersAfter, err := svc.Events.MessageEventMarkers(ctx, "session-1", sessionIndexSubject)
	if err != nil {
		t.Fatalf("MessageEventMarkers() (after) error = %v", err)
	}
	if markersAfter[sessionHeaderMessageID] == markersBefore[sessionHeaderMessageID] {
		t.Fatal("expected the header row's marker hash to change after the title changed")
	}
	if markersAfter["msg-1"] != markersBefore["msg-1"] {
		t.Fatal("expected msg-1's marker hash to stay the same (a title-only change must not force a per-message re-embed)")
	}
}
