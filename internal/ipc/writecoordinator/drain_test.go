// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package writecoordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
)

func createSessionRequest(t *testing.T, id string) dbproxy.WriteRequest {
	t.Helper()
	// MessageCount > 0 so ListSessions (which hides empty sessions) counts it.
	params, err := json.Marshal(db.CreateSessionParams{ID: id, Title: id, MessageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	return dbproxy.WriteRequest{Method: "CreateSession", Params: params}
}

// TestDrainAppliesQueuedWritesThenRejects verifies the first step of the
// primary handover: jobs queued before Drain complete, later Submits fail fast.
func TestDrainAppliesQueuedWritesThenRejects(t *testing.T) {
	conn, err := db.ConnectAt(filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	q := db.New(conn)

	c := New(context.Background(), q, 64)
	t.Cleanup(c.Shutdown)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Submit(context.Background(), createSessionRequest(t, fmt.Sprintf("s-%d", i))); err != nil {
				errs <- err
			}
		}()
	}
	// Wait until every job is queued, so all of them precede the barrier.
	deadline := time.Now().Add(5 * time.Second)
	for c.Metrics().Accepted < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("queued write failed: %v", err)
	}

	sessions, err := q.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != n {
		t.Fatalf("got %d sessions after Drain, want %d", len(sessions), n)
	}

	if _, err := c.Submit(context.Background(), createSessionRequest(t, "late")); !errors.Is(err, ErrDraining) {
		t.Fatalf("Submit after Drain = %v, want ErrDraining", err)
	}
	// Drain is idempotent and safe after Shutdown.
	if err := c.Drain(drainCtx); err != nil {
		t.Fatalf("second Drain: %v", err)
	}
	c.Shutdown()
	if err := c.Drain(drainCtx); err != nil {
		t.Fatalf("Drain after Shutdown: %v", err)
	}
}
