// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package writecoordinator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/writecoordinator"
)

// stubQuerier satisfies db.Querier with a no-op DeleteSession for testing.
type stubQuerier struct{ db.Querier }

func (s *stubQuerier) DeleteSession(_ context.Context, _ string) error { return nil }

func TestCoordinatorUnknownMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	coord := writecoordinator.New(ctx, &stubQuerier{}, 16)
	defer coord.Shutdown()

	req := dbproxy.WriteRequest{Method: "NoSuchMethod", Params: json.RawMessage(`null`)}
	_, err := coord.Submit(ctx, req)
	if err == nil {
		t.Fatal("expected error for unknown method, got nil")
	}

	m := coord.Metrics()
	if m.Failed != 1 {
		t.Fatalf("expected 1 failed job, got %d", m.Failed)
	}
	if m.Accepted != 1 {
		t.Fatalf("expected 1 accepted job, got %d", m.Accepted)
	}
}

func TestCoordinatorKnownMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	coord := writecoordinator.New(ctx, &stubQuerier{}, 16)
	defer coord.Shutdown()

	sessID, _ := json.Marshal("sess-test")
	req := dbproxy.WriteRequest{Method: "DeleteSession", Params: sessID}
	_, err := coord.Submit(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	m := coord.Metrics()
	if m.Completed != 1 {
		t.Fatalf("expected 1 completed job, got %d", m.Completed)
	}
}

func TestCoordinatorShutdown(t *testing.T) {
	ctx := context.Background()
	coord := writecoordinator.New(ctx, &stubQuerier{}, 8)
	coord.Shutdown()

	req := dbproxy.WriteRequest{Method: "DeleteSession", Params: json.RawMessage(`"sess-1"`)}
	_, err := coord.Submit(ctx, req)
	if err == nil {
		t.Fatal("expected error after shutdown, got nil")
	}
}

func TestCoordinatorContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	coord := writecoordinator.New(ctx, &stubQuerier{}, 8)
	defer coord.Shutdown()

	callCtx, callCancel := context.WithCancel(context.Background())
	callCancel() // cancelled before Submit

	req := dbproxy.WriteRequest{Method: "DeleteSession", Params: json.RawMessage(`"sess-2"`)}
	_, err := coord.Submit(callCtx, req)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
