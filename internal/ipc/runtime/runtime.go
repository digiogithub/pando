// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package runtime encapsulates the unified IPC bootstrap sequence that every
// Pando entrypoint must follow: derive ports, try the lock, open RW or RO DB,
// and wire services accordingly.
package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/changepub"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/failover"
	"github.com/digiogithub/pando/internal/logging"
)

const (
	// pingMethod is a lightweight RPC the primary answers to prove its RPC loop
	// is alive. A current primary replies immediately; an old primary that lacks
	// the handler still replies with a "method not found" error, which equally
	// proves liveness. Only a timeout (suspended/hung primary) means "no response".
	pingMethod = "ipc.ping"

	// stalePrimaryProbeTimeout is how long a freshly started secondary waits for
	// the existing primary to answer pingMethod before declaring it unresponsive
	// and killing it so this instance can take over.
	stalePrimaryProbeTimeout = 10 * time.Second
)

// Role describes the IPC role that this instance took during bootstrap.
type Role string

const (
	RolePrimary   Role = "primary"
	RoleSecondary Role = "secondary"
)

// BootstrapResult carries everything a caller needs after Bootstrap returns.
// Call Cleanup() on shutdown to release resources in correct order.
type BootstrapResult struct {
	Role Role

	// Querier is the db.Querier callers should use for all DB operations.
	// Primary: direct db.New(SQLDB). Secondary: a DBProxy that forwards writes
	// to the primary via ZMQ RPC and serves reads from the local RO connection.
	Querier db.Querier

	// SQLDB is the underlying *sql.DB. Primary holds a RW connection with WAL
	// pragmas applied and migrations run. Secondary holds a RO connection.
	SQLDB *sql.DB

	// Bus is non-nil only on the primary instance.
	Bus *ipc.Bus

	// IPCClient is non-nil only on the secondary instance.
	IPCClient *ipc.Client

	InstanceID string
	PubPort    int
	RPCPort    int

	// LockFile is the open flock file held by the primary, nil on secondary.
	LockFile *os.File

	// Watcher monitors primary liveness. Always non-nil; disabled by default (feature flag).
	// Callers can call Watcher.SetEnabled(true) to opt in to automatic failover.
	Watcher *failover.Watcher

	// Cleanup releases all resources acquired during Bootstrap in reverse order.
	// The caller MUST call this exactly once on shutdown.
	Cleanup func()
}

// Bootstrap runs the unified startup sequence for the given workdir.
//
//  1. Derive deterministic PUB/RPC ports from the path.
//  2. Attempt to acquire the exclusive IPC lock.
//  3. Primary: open RW DB (with migrations), create Bus, create direct Querier.
//  4. Secondary: open RO DB, create IPC Client, create DBProxy Querier.
//
// On lock error the function continues as primary so the caller does not lose
// functionality — consistent with the existing root.go behaviour.
func Bootstrap(ctx context.Context, workdir, instanceID string) (*BootstrapResult, error) {
	pubPort, rpcPort := ipc.PortsForPath(workdir)

	isPrimary, lockInfo, lockFile, lockErr := ipc.AcquireLock(workdir, instanceID, pubPort, rpcPort)
	if lockErr != nil {
		logging.Warn("IPC lock acquisition failed, continuing as primary without IPC", "error", lockErr)
	}

	// We acquired the secondary role, which means another process holds the lock.
	// That process may, however, be suspended (SIGSTOP) or hung while still
	// holding the flock — in which case this secondary would block forever on the
	// DB proxy. Probe the primary; if it does not answer within the timeout, kill
	// it and re-acquire the lock so we become the primary instead of hanging.
	if !isPrimary && lockErr == nil && lockInfo != nil {
		if killStalePrimary(ctx, workdir, lockInfo) {
			isPrimary, lockInfo, lockFile, lockErr = reacquireAfterKill(workdir, instanceID, pubPort, rpcPort)
		}
	}

	res := &BootstrapResult{
		InstanceID: instanceID,
		PubPort:    pubPort,
		RPCPort:    rpcPort,
		LockFile:   lockFile,
	}

	if isPrimary || lockErr != nil {
		res.Role = RolePrimary

		conn, err := db.Connect()
		if err != nil {
			if lockFile != nil {
				ipc.ReleaseLock(lockFile)
			}
			return nil, fmt.Errorf("ipc/runtime: open primary DB: %w", err)
		}

		logging.Info("IPC: role determined",
			"role", RolePrimary,
			"workdir", workdir,
			"instance_id", instanceID,
			"pub_port", pubPort,
			"rpc_port", rpcPort,
		)
		logging.Debug("IPC: primary DB opened", "role", RolePrimary, "workdir", workdir)

		bus := ipc.NewBus(instanceID)
		// Answer liveness probes from newly started secondaries so they can tell a
		// healthy primary apart from a suspended/hung one (see killStalePrimary).
		bus.RegisterMethod(pingMethod, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"pong"`), nil
		})

		watcher := failover.NewWatcherForPrimary(
			failover.DefaultConfig(),
			instanceID, workdir,
			pubPort, rpcPort,
			bus,
		)

		res.SQLDB = conn
		res.Querier = db.New(conn)
		res.Bus = bus
		res.Watcher = watcher

		res.Cleanup = func() {
			shutdownCtx := context.Background()
			watcher.Shutdown(shutdownCtx)
			if bus != nil {
				_ = bus.Shutdown()
			}
			_ = conn.Close()
			if lockFile != nil {
				ipc.ReleaseLock(lockFile)
			}
		}

		logging.Debug("IPC bootstrap: primary", "pubPort", pubPort, "rpcPort", rpcPort)
		return res, nil
	}

	// Secondary path: use the primary's ports from the lock file.
	res.Role = RoleSecondary
	res.PubPort = lockInfo.PubPort
	res.RPCPort = lockInfo.RPCPort

	logging.Info("IPC: role determined",
		"role", RoleSecondary,
		"workdir", workdir,
		"instance_id", instanceID,
		"pub_port", lockInfo.PubPort,
		"rpc_port", lockInfo.RPCPort,
	)

	// Open in RW+WAL mode so direct writes are attempted first.
	// busy_timeout=200ms lets SQLite retry briefly before returning BUSY,
	// at which point DBProxy falls back to the IPC proxy.
	rwConn, rwErr := db.ConnectRWSecondary()
	if rwErr != nil {
		// Cannot open DB — fall back gracefully with an empty cleanup.
		logging.Warn("IPC bootstrap: failed to open secondary RW DB, secondary has no DB", "error", rwErr)
		res.Cleanup = func() {}
		return res, nil
	}

	logging.Debug("IPC: secondary RW DB opened (WAL mode)", "role", RoleSecondary, "workdir", workdir)

	ipcClient, clientErr := ipc.NewClient(ctx)
	if clientErr != nil {
		_ = rwConn.Close()
		logging.Warn("IPC bootstrap: failed to create IPC client, secondary has no proxy", "error", clientErr)
		res.SQLDB = rwConn
		res.Querier = db.New(rwConn)
		res.Cleanup = func() { _ = rwConn.Close() }
		return res, nil
	}

	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.RPCPort)
	proxy := dbproxy.New(db.New(rwConn), ipcClient, rpcAddr)

	res.SQLDB = rwConn
	res.Querier = proxy
	res.IPCClient = ipcClient

	// Subscribe to write-change events published by the primary so this
	// secondary can react (cache invalidation, view refresh, etc.) without polling.
	pubAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.PubPort)
	changeCh, subErr := ipcClient.SubscribeTo(pubAddr,
		"db.session.", "db.message.", "db.file.", "db.project.", "db.skill.")
	if subErr != nil {
		logging.Warn("IPC: secondary failed to subscribe to write-change events", "error", subErr)
	} else {
		go handleWriteChanges(ctx, changeCh)
	}

	// Create a failover watcher for this secondary. Auto-failover is disabled by default;
	// the caller can enable it with Watcher.SetEnabled(true) or via --auto-failover.
	watcher := failover.NewWatcherForSecondary(
		failover.DefaultConfig(),
		instanceID, workdir,
		pubPort, rpcPort,
		ipcClient,
		pubAddr,
		nil, // nil → defaultPromoteStub; Phase 5b will wire real promotion logic
	)
	res.Watcher = watcher
	// Start the secondary watcher immediately; it will monitor primary heartbeats and
	// act if the primary dies. Disabled by default — safe even if started early.
	watcher.Start(ctx)

	res.Cleanup = func() {
		shutdownCtx := context.Background()
		watcher.Shutdown(shutdownCtx)
		_ = ipcClient.Close()
		_ = rwConn.Close()
	}

	logging.Info("IPC: secondary connected to primary",
		"role", RoleSecondary,
		"workdir", workdir,
		"instance_id", instanceID,
		"pub_port", lockInfo.PubPort,
		"rpc_port", lockInfo.RPCPort,
		"primary_rpc", rpcAddr,
	)
	logging.Debug("IPC bootstrap: secondary",
		"primaryPub", fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.PubPort),
		"primaryRPC", rpcAddr)
	return res, nil
}

// killStalePrimary probes the primary recorded in lockInfo. If it does not
// respond to pingMethod within stalePrimaryProbeTimeout (e.g. it is suspended or
// hung while still holding the flock), the primary process is killed so this
// instance can take over. Returns true when the primary was killed and the lock
// should be re-acquired.
func killStalePrimary(ctx context.Context, workdir string, lockInfo *ipc.LockInfo) bool {
	if lockInfo == nil || lockInfo.PID <= 0 || lockInfo.PID == os.Getpid() {
		return false
	}

	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.RPCPort)
	if primaryResponds(ctx, rpcAddr) {
		return false
	}

	logging.Warn("IPC: primary did not respond within timeout, killing it to take over",
		"workdir", workdir,
		"primary_pid", lockInfo.PID,
		"primary_instance", lockInfo.InstanceID,
		"timeout", stalePrimaryProbeTimeout,
	)

	if err := killProcess(lockInfo.PID); err != nil {
		logging.Warn("IPC: failed to kill unresponsive primary", "pid", lockInfo.PID, "error", err)
		return false
	}

	// Wait for the killed process to fully exit so the kernel releases the flock.
	waitForProcessExit(lockInfo.PID, 2*time.Second)
	return true
}

// primaryResponds reports whether the primary at rpcAddr answers an RPC within
// stalePrimaryProbeTimeout. Any reply — including a "method not found" error from
// an older primary that lacks the ping handler — counts as alive; only a timeout
// (or inability to reach the RPC loop at all) counts as unresponsive.
func primaryResponds(ctx context.Context, rpcAddr string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, stalePrimaryProbeTimeout)
	defer cancel()

	client, err := ipc.NewClient(probeCtx)
	if err != nil {
		// We could not even build a probe client; be conservative and assume the
		// primary is alive so a local fault never causes us to kill it.
		logging.Warn("IPC: failed to create probe client; assuming primary alive", "error", err)
		return true
	}
	defer client.Close()

	if _, err := client.Call(probeCtx, rpcAddr, pingMethod, nil); err != nil {
		// A method-not-found reply still proves the RPC loop is serving.
		if errors.Is(err, ipc.ErrMethodNotFound) {
			return true
		}
		// A timeout means the primary is suspended/hung. Treat any other transport
		// failure the same way: the primary holds the lock but cannot serve us, so
		// continuing as secondary would hang.
		logging.Warn("IPC: primary probe failed", "rpc", rpcAddr, "error", err)
		return false
	}
	return true
}

// killProcess sends SIGKILL to pid. SIGKILL (not SIGTERM) is required because a
// SIGSTOP-suspended process will not act on catchable signals until resumed,
// whereas SIGKILL cannot be blocked, caught, or ignored.
func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// waitForProcessExit blocks until pid is gone or the deadline elapses.
func waitForProcessExit(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// processAlive reports whether pid refers to a live process.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 performs error checking only: nil means the process exists.
	return proc.Signal(syscall.Signal(0)) == nil
}

// reacquireAfterKill retries AcquireLock after a stale primary was killed. The
// kernel releases the dead process's flock asynchronously during teardown, so we
// poll briefly until we win the lock (become primary) or hit a hard error.
func reacquireAfterKill(workdir, instanceID string, pubPort, rpcPort int) (bool, *ipc.LockInfo, *os.File, error) {
	var (
		isPrimary bool
		lockInfo  *ipc.LockInfo
		lockFile  *os.File
		lockErr   error
	)
	deadline := time.Now().Add(2 * time.Second)
	for {
		isPrimary, lockInfo, lockFile, lockErr = ipc.AcquireLock(workdir, instanceID, pubPort, rpcPort)
		if isPrimary || lockErr != nil || time.Now().After(deadline) {
			return isPrimary, lockInfo, lockFile, lockErr
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// handleWriteChanges drains the change-event channel published by the primary
// and logs each event. Future phases will use these events for cache invalidation
// and active-view refresh without polling.
func handleWriteChanges(ctx context.Context, ch <-chan ipc.Envelope) {
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			var change changepub.WriteChange
			if err := json.Unmarshal(env.Payload, &change); err != nil {
				logging.Debug("IPC: failed to unmarshal write-change event", "error", err)
				continue
			}
			logging.Debug("IPC: write change received",
				"topic", change.Topic,
				"source", change.InstanceID,
			)
		}
	}
}
