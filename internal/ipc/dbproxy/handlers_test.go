// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
)

type busRecorder struct {
	handler ipc.HandlerFunc
}

func (b *busRecorder) RegisterMethod(_ string, handler ipc.HandlerFunc) {
	b.handler = handler
}

type recordingQuerier struct {
	db.Querier
	createSessionCalled bool
	createSessionArg    db.CreateSessionParams
	createSessionResult db.Session
	createSessionErr    error
}

func (r *recordingQuerier) CreateSession(ctx context.Context, arg db.CreateSessionParams) (db.Session, error) {
	r.createSessionCalled = true
	r.createSessionArg = arg
	if r.createSessionErr != nil {
		return db.Session{}, r.createSessionErr
	}
	return r.createSessionResult, nil
}

func TestRegisterHandlers_RegistersDBWriteBeforeBusStart(t *testing.T) {
	bus := &busRecorder{}
	querier := &recordingQuerier{
		createSessionResult: db.Session{ID: "sess-123", Title: "non-interactive"},
	}

	RegisterHandlers(bus, querier)

	payload, err := json.Marshal(db.CreateSessionParams{ID: "sess-123", Title: "non-interactive"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	request, err := json.Marshal(WriteRequest{Method: "CreateSession", Params: payload})
	if err != nil {
		t.Fatalf("marshal write request: %v", err)
	}

	handler := bus.handler
	if handler == nil {
		t.Fatal("expected db.write handler to be registered")
	}

	raw, err := handler(context.Background(), MethodDBWrite, request)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !querier.createSessionCalled {
		t.Fatal("expected CreateSession to be dispatched")
	}
	if querier.createSessionArg.Title != "non-interactive" {
		t.Fatalf("expected CreateSession title to round-trip, got %q", querier.createSessionArg.Title)
	}

	var got db.Session
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.ID != querier.createSessionResult.ID {
		t.Fatalf("expected session ID %q, got %q", querier.createSessionResult.ID, got.ID)
	}
}

func TestRegisterHandlers_PropagatesQuerierErrors(t *testing.T) {
	bus := &busRecorder{}
	querier := &recordingQuerier{createSessionErr: errors.New("boom")}

	RegisterHandlers(bus, querier)

	payload, err := json.Marshal(db.CreateSessionParams{ID: "sess-123", Title: "non-interactive"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	request, err := json.Marshal(WriteRequest{Method: "CreateSession", Params: payload})
	if err != nil {
		t.Fatalf("marshal write request: %v", err)
	}

	handler := bus.handler
	if handler == nil {
		t.Fatal("expected db.write handler to be registered")
	}

	_, err = handler(context.Background(), MethodDBWrite, request)
	if err == nil {
		t.Fatal("expected handler to return error")
	}
	if err.Error() != "boom" {
		t.Fatalf("expected original error, got %v", err)
	}
}
