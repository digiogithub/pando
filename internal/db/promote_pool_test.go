package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// pragmaOnEveryConn checks out n connections at once (so it sees every
// physical connection of a pool capped at n) and returns the value of pragma
// on each.
func pragmaOnEveryConn(t *testing.T, sqlDB *sql.DB, n int, pragma string) []int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conns := make([]*sql.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	values := make([]int, 0, n)
	for range n {
		c, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatalf("check out connection: %v", err)
		}
		conns = append(conns, c)
		var v int
		if err := c.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&v); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		values = append(values, v)
	}
	return values
}

// TestPromoteToPrimaryPoolReconfiguresEveryConnection covers the in-place
// promotion of a secondary pool: the connection that existed before (and was
// in use while the promotion started) must end up with the primary's
// busy_timeout, like the ones opened afterwards, and migrations must run.
func TestPromoteToPrimaryPoolReconfiguresEveryConnection(t *testing.T) {
	sec, err := ConnectRWSecondaryAt(filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("ConnectRWSecondaryAt: %v", err)
	}
	t.Cleanup(func() { _ = sec.Close() })

	if got := pragmaOnEveryConn(t, sec, 1, "busy_timeout"); got[0] != 200 {
		t.Fatalf("secondary busy_timeout = %d, want 200", got[0])
	}

	// Hold the pool's only connection while promotion starts: reconfigurePool
	// must wait for it rather than skip it.
	ctx := context.Background()
	held, err := sec.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- PromoteToPrimaryPool(ctx, sec) }()
	select {
	case err := <-done:
		t.Fatalf("promotion finished while a connection was still in use (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	_ = held.Close()
	if err := <-done; err != nil {
		t.Fatalf("PromoteToPrimaryPool: %v", err)
	}

	if got := sec.Stats().MaxOpenConnections; got != primaryMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, primaryMaxOpenConns)
	}
	for i, v := range pragmaOnEveryConn(t, sec, primaryMaxOpenConns, "busy_timeout") {
		if v != int(primaryBusyTimeout/time.Millisecond) {
			t.Errorf("connection %d busy_timeout = %d, want %d", i, v, primaryBusyTimeout/time.Millisecond)
		}
	}
	for i, v := range pragmaOnEveryConn(t, sec, primaryMaxOpenConns, "foreign_keys") {
		if v != 1 {
			t.Errorf("connection %d foreign_keys = %d, want 1", i, v)
		}
	}
	var n int
	if err := sec.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("migrations did not run on the promoted pool (sessions table count=%d, err=%v)", n, err)
	}

	// Demotion (used to roll back a failed promotion) restores secondary settings.
	if err := DemoteToSecondaryPool(ctx, sec); err != nil {
		t.Fatalf("DemoteToSecondaryPool: %v", err)
	}
	if got := sec.Stats().MaxOpenConnections; got != secondaryMaxOpenConns {
		t.Fatalf("MaxOpenConnections after demotion = %d, want %d", got, secondaryMaxOpenConns)
	}
	if got := pragmaOnEveryConn(t, sec, 1, "busy_timeout"); got[0] != 200 {
		t.Fatalf("busy_timeout after demotion = %d, want 200", got[0])
	}
}

func TestReconfigurePoolRejectsForeignPool(t *testing.T) {
	foreign, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver not registered under that name: %v", err)
	}
	defer foreign.Close()
	if err := PromoteToPrimaryPool(context.Background(), foreign); err == nil {
		t.Fatal("PromoteToPrimaryPool accepted a pool not opened by openPool")
	}
}
