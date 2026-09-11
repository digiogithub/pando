// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/dbproxy/proxytest"
)

func sessionExists(t *testing.T, tp *proxytest.Topology, id string) bool {
	t.Helper()
	var n int
	if err := tp.PrimaryDB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func newSessionParams() db.CreateSessionParams {
	return db.CreateSessionParams{ID: uuid.NewString(), Title: "p5 handover"}
}

// A forwarded write issued while the primary is draining for a handover, and
// whose primary then disappears, succeeds once a new primary serves the same
// endpoint (the common case: canonical workdir → identical ports).
func TestForwardedWriteSurvivesGracefulHandover(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()

	// Sanity: forwarding works.
	p0 := newSessionParams()
	if _, forwarded, err := dbproxy.ForwardWithResult[db.Session](ctx, tp.Proxy, "CreateSession", p0); err != nil || !forwarded {
		t.Fatalf("baseline forward: forwarded=%v err=%v", forwarded, err)
	}

	// Old primary drains (refuses new writes with ErrDraining), then goes
	// away; a new primary binds the same ports 800 ms later.
	tp.DrainPrimary(t)
	go func() {
		time.Sleep(300 * time.Millisecond)
		tp.StopPrimary()
		time.Sleep(500 * time.Millisecond)
		tp.StartPrimary(t)
	}()

	started := time.Now()
	typed := newSessionParams()
	_, forwarded, err := dbproxy.ForwardWithResult[db.Session](ctx, tp.Proxy, "CreateSession", typed)
	if err != nil || !forwarded {
		t.Fatalf("typed forward across handover: forwarded=%v err=%v", forwarded, err)
	}
	if elapsed := time.Since(started); elapsed < 700*time.Millisecond {
		t.Fatalf("write returned after %s, before the new primary existed", elapsed)
	}
	if !sessionExists(t, tp, typed.ID) {
		t.Fatal("typed write not applied by the new primary")
	}

	// A void write (the remembrances Forward path) across a second handover.
	tp.DrainPrimary(t)
	go func() {
		time.Sleep(200 * time.Millisecond)
		tp.StopPrimary()
		time.Sleep(400 * time.Millisecond)
		tp.StartPrimary(t)
	}()
	if forwarded, err := tp.Proxy.Forward(ctx, "DeleteSession", typed.ID, dbproxy.DefaultWriteTimeouts.Default); err != nil || !forwarded {
		t.Fatalf("void forward across handover: forwarded=%v err=%v", forwarded, err)
	}
	if sessionExists(t, tp, typed.ID) {
		t.Fatal("void write not applied by the new primary")
	}
}

// With no primary coming back, the wait is bounded and the error says why.
func TestForwardedWriteHandoverWaitIsBounded(t *testing.T) {
	tp := proxytest.New(t)
	tp.Proxy.SetHandoverWait(1500 * time.Millisecond)
	tp.StopPrimary()

	started := time.Now()
	forwarded, err := tp.Proxy.Forward(context.Background(), "DeleteSession", "nope", dbproxy.DefaultWriteTimeouts.Default)
	elapsed := time.Since(started)
	if !forwarded || err == nil {
		t.Fatalf("expected a forwarded failure, got forwarded=%v err=%v", forwarded, err)
	}
	var werr *dbproxy.WriteError
	if !errors.As(err, &werr) || !werr.IsRetryable() {
		t.Fatalf("error must still carry the retryable *WriteError: %v", err)
	}
	if werr.Code == dbproxy.ErrCodeUnreachable && !strings.Contains(err.Error(), "no primary accepted the write within") {
		t.Fatalf("exhausted wait must be explained: %v", err)
	}
	// Bound + one in-flight call timeout (5 s) at most.
	if elapsed > 1500*time.Millisecond+dbproxy.DefaultWriteTimeouts.Default+time.Second {
		t.Fatalf("waited %s, beyond the bound", elapsed)
	}
	t.Logf("gave up after %s: %v", elapsed, err)
}

// If this instance itself wins the promotion while a forwarded write waits,
// the write is performed locally instead (Forward reports forwarded=false).
func TestForwardedWriteDuringWaitSwitchesToLocalAfterPromotion(t *testing.T) {
	tp := proxytest.New(t)
	tp.StopPrimary()
	go func() {
		time.Sleep(400 * time.Millisecond)
		tp.Proxy.Promote()
	}()
	forwarded, err := tp.Proxy.Forward(context.Background(), "DeleteSession", "x", dbproxy.DefaultWriteTimeouts.Default)
	if forwarded || err != nil {
		t.Fatalf("after promotion the caller must write locally: forwarded=%v err=%v", forwarded, err)
	}
}

// The lock-file resolver re-points the proxy when the new primary serves on a
// different endpoint.
func TestForwardFollowsResolverToNewEndpoint(t *testing.T) {
	old := proxytest.New(t)
	next := proxytest.New(t) // a second primary on other ports, same kind of DB
	// Point old's proxy at a dead endpoint first by stopping its primary, and
	// make the resolver report next's endpoint.
	old.StopPrimary()
	dbproxy.RegisterStatementExecutor(next.PrimaryDB)
	proxy := dbproxy.New(db.New(next.SecondaryDB), old.Client, old.RPCAddr())
	proxy.SetPrimaryResolver(func() (string, bool) { return next.RPCAddr(), true })

	p := newSessionParams()
	if _, forwarded, err := dbproxy.ForwardWithResult[db.Session](context.Background(), proxy, "CreateSession", p); err != nil || !forwarded {
		t.Fatalf("forward: forwarded=%v err=%v", forwarded, err)
	}
	if proxy.RPCAddr() != next.RPCAddr() {
		t.Fatalf("proxy not re-pointed: %s", proxy.RPCAddr())
	}
	if !sessionExists(t, next, p.ID) {
		t.Fatal("write not applied by the resolved primary")
	}
}

// SQLWriter: a statement write that loses the lock race on the secondary is
// forwarded to the primary, which applies it once its long write finishes.
func TestSQLWriterForwardsOnLockContention(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	w := dbproxy.NewSQLWriter(tp.SecondaryDB, tp.Proxy)
	insert := dbproxy.RegisterStatement("dbproxy_ext_test.insert_session",
		`INSERT INTO sessions (id, title, message_count, prompt_tokens, completion_tokens, cost, updated_at, created_at)
		 VALUES (?, ?, 0, 0, 0, 0, strftime('%s','now'), strftime('%s','now'))`)

	released := tp.HoldWriteLock(t, time.Second)
	started := time.Now()
	id := uuid.NewString()
	n, err := w.Exec(ctx, insert, id, "via forward")
	if err != nil || n != 1 {
		t.Fatalf("Exec under primary lock: n=%d err=%v", n, err)
	}
	if time.Since(started) < 800*time.Millisecond {
		t.Fatalf("returned in %s while the lock was held", time.Since(started))
	}
	<-released
	if !sessionExists(t, tp, id) {
		t.Fatal("forwarded statement not applied")
	}

	// A primary that cannot execute statements (older binary): bounded
	// direct retry on the secondary pool.
	dbproxy.RegisterStatementExecutor(nil)
	tp.HoldWriteLock(t, 600*time.Millisecond)
	id2 := uuid.NewString()
	if _, err := w.Exec(ctx, insert, id2, "direct retry"); err != nil {
		t.Fatalf("direct-retry fallback: %v", err)
	}
	if !sessionExists(t, tp, id2) {
		t.Fatal("direct-retry write not applied")
	}

	// After promotion the writer is a plain direct writer.
	tp.Proxy.Promote()
	id3 := uuid.NewString()
	if _, err := w.Exec(ctx, insert, id3, "promoted"); err != nil || !sessionExists(t, tp, id3) {
		t.Fatalf("promoted direct write: %v", err)
	}
}
