package agui

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/dbproxy/proxytest"
)

// The adapter's thread map degrades to memory-only after the first
// persistence failure. On an IPC secondary the shared pool is bound to the
// app's write proxy, so an upsert that loses the lock race is forwarded to
// the primary instead of permanently degrading the store.
func TestThreadStorePersistsThroughProxyOnSecondary(t *testing.T) {
	tp := proxytest.New(t)
	dbproxy.BindPool(tp.SecondaryDB, tp.Proxy)
	t.Cleanup(func() { dbproxy.BindPool(tp.SecondaryDB, nil) })

	ctx := context.Background()
	store := newThreadStore(tp.SecondaryDB)

	tp.HoldWriteLock(t, 700*time.Millisecond)
	started := time.Now()
	store.put(ctx, "thread-1", "session-1", "coder")
	if time.Since(started) < 500*time.Millisecond {
		t.Fatalf("put returned in %s while the lock was held (not forwarded?)", time.Since(started))
	}
	if store.degraded.Load() {
		t.Fatal("thread store degraded to memory-only instead of forwarding")
	}
	var sessionID string
	if err := tp.PrimaryDB.QueryRow(`SELECT session_id FROM agui_threads WHERE thread_id = 'thread-1'`).Scan(&sessionID); err != nil {
		t.Fatalf("row not persisted: %v", err)
	}
	if sessionID != "session-1" {
		t.Fatalf("session_id = %q", sessionID)
	}

	// A fresh store (a restarted process) reads the binding back.
	if got, ok := newThreadStore(tp.SecondaryDB).get(ctx, "thread-1"); !ok || got != "session-1" {
		t.Fatalf("get = %q,%v", got, ok)
	}

	tp.HoldWriteLock(t, 400*time.Millisecond)
	store.forget(ctx, "thread-1")
	if store.degraded.Load() {
		t.Fatal("forget degraded the store")
	}
	var n int
	if err := tp.PrimaryDB.QueryRow(`SELECT COUNT(*) FROM agui_threads WHERE thread_id = 'thread-1'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("row still present: n=%d err=%v", n, err)
	}
}
