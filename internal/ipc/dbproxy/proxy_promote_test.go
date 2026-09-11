// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
)

// deadPrimaryAddr is an RPC endpoint nothing listens on. Tests below never
// send to it while the proxy is remote; they only need a non-nil client.
const deadPrimaryAddr = "tcp://127.0.0.1:1"

func openPromoteTestProxy(t *testing.T) (*DBProxy, *ipc.Client, db.Querier) {
	t.Helper()
	conn, err := db.ConnectAt(filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client, err := ipc.NewClient(context.Background())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	q := db.New(conn)
	return New(q, client, deadPrimaryAddr), client, q
}

func TestIsRemoteIsNilSafe(t *testing.T) {
	var p *DBProxy
	if p.IsRemote() {
		t.Fatal("nil proxy reports remote")
	}
	forwarded, err := p.Forward(context.Background(), "KBDeleteDocument", "x", time.Second)
	if forwarded || err != nil {
		t.Fatalf("nil proxy Forward = (%v, %v), want (false, nil)", forwarded, err)
	}
	_, forwarded, err = ForwardWithResult[int64](context.Background(), p, "SaveEvent", nil)
	if forwarded || err != nil {
		t.Fatalf("nil proxy ForwardWithResult = (%v, %v), want (false, nil)", forwarded, err)
	}
	if New(nil, nil, "").IsRemote() {
		t.Fatal("proxy without a client reports remote")
	}
}

func TestPromoteFlipsToPassthrough(t *testing.T) {
	p, client, _ := openPromoteTestProxy(t)
	if !p.IsRemote() {
		t.Fatal("proxy with a client must report remote before Promote")
	}

	if got := p.Promote(); got != client {
		t.Fatalf("Promote returned %p, want the proxy's client %p", got, client)
	}
	if p.IsRemote() {
		t.Fatal("proxy still remote after Promote")
	}
	if got := p.Promote(); got != nil {
		t.Fatal("second Promote must return nil (already passthrough)")
	}

	ctx := context.Background()
	if err := p.WriteWithRetry(ctx, "KBDeleteDocument", "x", time.Second); !errors.Is(err, ErrNotRemote) {
		t.Fatalf("WriteWithRetry after Promote = %v, want ErrNotRemote", err)
	}
	if forwarded, err := p.Forward(ctx, "KBDeleteDocument", "x", time.Second); forwarded || err != nil {
		t.Fatalf("Forward after Promote = (%v, %v), want (false, nil)", forwarded, err)
	}
	if err := p.ProbePrimary(ctx); err != nil {
		t.Fatalf("ProbePrimary after Promote = %v, want nil", err)
	}
	if _, err := p.CreateSession(ctx, db.CreateSessionParams{ID: "s-after-promote", Title: "t"}); err != nil {
		t.Fatalf("direct CreateSession after Promote: %v", err)
	}
}

// TestConcurrentWritesDuringPromote runs writers through the proxy while it is
// promoted (run with -race): every write must succeed and the client pointer
// must be read/swapped without a data race.
func TestConcurrentWritesDuringPromote(t *testing.T) {
	p, _, q := openPromoteTestProxy(t)
	ctx := context.Background()

	const writers, perWriter = 6, 25
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	start := make(chan struct{})
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range perWriter {
				_ = p.IsRemote()
				id := fmt.Sprintf("s-%d-%d", w, i)
				// MessageCount > 0 so ListSessions (which hides empty sessions) counts it.
				if _, err := p.CreateSession(ctx, db.CreateSessionParams{ID: id, Title: id, MessageCount: 1}); err != nil {
					errs <- err
				}
			}
		}()
	}
	close(start)
	time.Sleep(time.Millisecond)
	p.Promote()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("write during promote failed: %v", err)
	}

	sessions, err := q.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != writers*perWriter {
		t.Fatalf("got %d sessions, want %d", len(sessions), writers*perWriter)
	}
	if p.IsRemote() {
		t.Fatal("proxy remote after Promote")
	}
}
