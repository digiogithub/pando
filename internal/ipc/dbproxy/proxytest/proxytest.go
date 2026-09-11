// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package proxytest builds a real two-role IPC write topology for tests: a
// migrated database file, the primary's pool with a write coordinator served
// over a real ZMQ bus, and a secondary's 1-connection 200 ms pool with a
// DBProxy forwarding to that bus. It is imported only by tests.
package proxytest

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/writecoordinator"
)

// Topology is a primary + secondary pair sharing one database file.
type Topology struct {
	DBPath string
	// PrimaryDB is the primary's pool (10 s busy timeout, migrated). The
	// coordinator and the statement executor run on it.
	PrimaryDB *sql.DB
	// SecondaryDB is the secondary's pool (1 connection, 200 ms busy timeout).
	SecondaryDB *sql.DB
	// Proxy is the secondary's DBProxy: local querier on SecondaryDB, writes
	// forwarded to the primary's bus on lock contention.
	Proxy  *dbproxy.DBProxy
	Client *ipc.Client

	PubPort, RPCPort int

	mu    sync.Mutex
	bus   *ipc.Bus
	coord *writecoordinator.Coordinator
	stop  context.CancelFunc
}

// New builds the topology and starts the primary. Everything is torn down by
// t.Cleanup, including the package-level statement executor.
func New(t testing.TB) *Topology {
	t.Helper()
	tp := &Topology{DBPath: filepath.Join(t.TempDir(), "pando.db")}
	var err error
	if tp.PrimaryDB, err = db.ConnectAt(tp.DBPath); err != nil {
		t.Fatalf("proxytest: ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = tp.PrimaryDB.Close() })
	if tp.SecondaryDB, err = db.ConnectRWSecondaryAt(tp.DBPath); err != nil {
		t.Fatalf("proxytest: ConnectRWSecondaryAt: %v", err)
	}
	t.Cleanup(func() { _ = tp.SecondaryDB.Close() })

	if tp.PubPort, tp.RPCPort, err = ipc.FindFreePorts(); err != nil {
		t.Fatalf("proxytest: FindFreePorts: %v", err)
	}
	if tp.Client, err = ipc.NewClient(context.Background()); err != nil {
		t.Fatalf("proxytest: NewClient: %v", err)
	}
	t.Cleanup(func() { _ = tp.Client.Close() })
	tp.Proxy = dbproxy.New(db.New(tp.SecondaryDB), tp.Client, tp.RPCAddr())

	t.Cleanup(func() { dbproxy.RegisterStatementExecutor(nil) })
	tp.StartPrimary(t)
	t.Cleanup(tp.StopPrimary)
	return tp
}

// RPCAddr is the primary endpoint the proxy forwards to.
func (tp *Topology) RPCAddr() string {
	return fmt.Sprintf("tcp://127.0.0.1:%d", tp.RPCPort)
}

// StartPrimary starts a (new) primary bus with a fresh write coordinator on
// PrimaryDB, bound to the topology's ports; it retries the bind for up to 3 s
// because a just-stopped bus may still own them.
func (tp *Topology) StartPrimary(t testing.TB) {
	t.Helper()
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tp.bus != nil {
		t.Fatalf("proxytest: primary already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	coord := writecoordinator.New(ctx, db.New(tp.PrimaryDB), 64)
	bus := ipc.NewBus("proxytest-primary")
	dbproxy.RegisterHandlersWithCoordinator(bus, coord)
	dbproxy.RegisterStatementExecutor(tp.PrimaryDB)

	deadline := time.Now().Add(3 * time.Second)
	for {
		err := bus.Start(ctx, tp.PubPort, tp.RPCPort)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			coord.Shutdown()
			cancel()
			t.Fatalf("proxytest: start primary bus: %v", err)
		}
		_ = bus.Shutdown()
		bus = ipc.NewBus("proxytest-primary")
		dbproxy.RegisterHandlersWithCoordinator(bus, coord)
		time.Sleep(50 * time.Millisecond)
	}
	tp.bus, tp.coord, tp.stop = bus, coord, cancel
}

// DrainPrimary makes the running primary refuse new writes the way a primary
// handing over does (writecoordinator.ErrDraining), without closing its bus.
func (tp *Topology) DrainPrimary(t testing.TB) {
	t.Helper()
	tp.mu.Lock()
	coord := tp.coord
	tp.mu.Unlock()
	if coord == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := coord.Drain(ctx); err != nil {
		t.Fatalf("proxytest: drain: %v", err)
	}
}

// StopPrimary drains the coordinator and shuts the bus down (idempotent).
func (tp *Topology) StopPrimary() {
	tp.mu.Lock()
	bus, coord, stop := tp.bus, tp.coord, tp.stop
	tp.bus, tp.coord, tp.stop = nil, nil, nil
	tp.mu.Unlock()
	if coord != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = coord.Drain(ctx)
		cancel()
		coord.Shutdown()
	}
	if bus != nil {
		_ = bus.Shutdown()
	}
	if stop != nil {
		stop()
	}
}

// HoldWriteLock takes the database write lock on a primary connection
// (BEGIN IMMEDIATE) and releases it after d, simulating the primary holding
// a long write. It returns once the lock is held; the returned channel is
// closed after the release.
func (tp *Topology) HoldWriteLock(t testing.TB, d time.Duration) <-chan struct{} {
	t.Helper()
	ctx := context.Background()
	conn, err := tp.PrimaryDB.Conn(ctx)
	if err != nil {
		t.Fatalf("proxytest: conn: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		t.Fatalf("proxytest: BEGIN IMMEDIATE: %v", err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(d)
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		_ = conn.Close()
	}()
	t.Cleanup(func() { <-released })
	return released
}
