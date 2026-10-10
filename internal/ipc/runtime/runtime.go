// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package runtime encapsulates the unified IPC bootstrap sequence that every
// Pando entrypoint follows: open the database, derive ports, try the IPC lock,
// and wire the leader or follower role accordingly.
//
// The IPC role no longer has anything to do with database writes: every
// instance opens pando.db read-write through the multi-writer engine
// (internal/db). The lock only elects the leader that runs the singleton
// background jobs (code index and its watcher, evaluator sweeps) and serves the
// IPC bus (instance browsing, remote view, hot-peer delegation).
package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/failover"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/sandbox/portguard"
)

// pingMethod is a lightweight RPC the leader answers to prove its RPC loop is
// alive (used by liveness probes and `pando db compact` of older versions).
const pingMethod = "ipc.ping"

// Role describes the IPC role that this instance took during bootstrap.
type Role string

const (
	// RolePrimary is the leader: it holds the IPC lock and serves the bus.
	RolePrimary Role = "primary"
	// RoleSecondary is a follower: another process holds the IPC lock.
	RoleSecondary Role = "secondary"
)

// BootstrapResult carries everything a caller needs after Bootstrap returns.
// Call Cleanup() on shutdown to release resources in correct order.
type BootstrapResult struct {
	Role Role

	// Querier is the db.Querier callers should use for all DB operations: a
	// direct querier on SQLDB in every role.
	Querier db.Querier

	// SQLDB is the read-write connection pool on pando.db (migrations applied).
	SQLDB *sql.DB

	// Bus is non-nil only on the leader.
	Bus *ipc.Bus

	// IPCClient is non-nil only on followers (heartbeat subscription).
	IPCClient *ipc.Client

	InstanceID string
	PubPort    int
	RPCPort    int

	// LockFile is the open flock file held by the leader, nil on followers.
	LockFile *os.File

	// Watcher monitors leader liveness. Always non-nil.
	Watcher *failover.Watcher

	// Cleanup releases all resources acquired during Bootstrap in reverse order.
	// The caller MUST call this exactly once on shutdown.
	Cleanup func()
}

// Bootstrap runs the unified startup sequence for the given workdir.
//
//  1. Open pando.db read-write through the engine (migrations serialised
//     across processes by internal/db).
//  2. Derive deterministic PUB/RPC ports from the path.
//  3. Attempt to acquire the exclusive IPC lock.
//  4. Leader: create the Bus and the leader watcher.
//  5. Follower: create the IPC client and a follower watcher.
//
// On lock error the function continues as leader so the caller does not lose
// functionality — consistent with the existing root.go behaviour.
func Bootstrap(ctx context.Context, workdir, instanceID string) (*BootstrapResult, error) {
	conn, err := db.Connect()
	if err != nil {
		return nil, fmt.Errorf("ipc/runtime: open DB: %w", err)
	}
	closeDB := func() { _ = db.Close(conn) }

	pubPort, rpcPort := ipc.PortsForPath(workdir)

	isPrimary, lockInfo, lockFile, lockErr := ipc.AcquireLock(workdir, instanceID, pubPort, rpcPort)
	if errors.Is(lockErr, ipc.ErrPrimaryLockHeld) {
		// Another process holds the lock but we could not read its ports. Retry once
		// in case we simply raced with the leader rewriting the file.
		time.Sleep(250 * time.Millisecond)
		isPrimary, lockInfo, lockFile, lockErr = ipc.AcquireLock(workdir, instanceID, pubPort, rpcPort)
	}
	if errors.Is(lockErr, ipc.ErrPrimaryLockHeld) {
		// Never fall through to the leader branch here: that would bind a second
		// bus and run the singleton jobs twice.
		logging.Error("IPC: another instance holds the lock but its lock file is unreadable; refusing to start a second leader",
			"workdir", workdir,
			"lock_file", filepath.Join(workdir, ".pando", "ipc.lock"),
			"error", lockErr,
			"hint", "if no other pando process is running for this directory, delete .pando/ipc.lock",
		)
		closeDB()
		return nil, fmt.Errorf("ipc/runtime: %w", lockErr)
	}
	if lockErr != nil {
		logging.Warn("IPC lock acquisition failed, continuing as leader without IPC", "error", lockErr)
	}

	res := &BootstrapResult{
		InstanceID: instanceID,
		PubPort:    pubPort,
		RPCPort:    rpcPort,
		LockFile:   lockFile,
		SQLDB:      conn,
		Querier:    db.New(conn),
	}

	if isPrimary || lockErr != nil {
		res.Role = RolePrimary

		logging.Info("IPC: role determined",
			"role", RolePrimary,
			"workdir", workdir,
			"instance_id", instanceID,
			"pub_port", pubPort,
			"rpc_port", rpcPort,
		)

		bus := ipc.NewBus(instanceID)
		// Answer liveness probes from followers and CLI tools.
		bus.RegisterMethod(pingMethod, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"pong"`), nil
		})

		watcher := failover.NewWatcherForPrimary(
			failover.DefaultConfig(),
			instanceID, workdir,
			pubPort, rpcPort,
			bus,
		)

		res.Bus = bus
		res.Watcher = watcher

		res.Cleanup = func() {
			shutdownCtx := context.Background()
			watcher.Shutdown(shutdownCtx)
			if bus != nil {
				_ = bus.Shutdown()
			}
			closeDB()
			if lockFile != nil {
				ipc.ReleaseLock(lockFile)
			}
		}
		return res, nil
	}

	// Follower path: use the leader's ports from the lock file.
	res.Role = RoleSecondary
	res.PubPort = lockInfo.PubPort
	res.RPCPort = lockInfo.RPCPort

	// The leader publishes its bus ports in the shared guarded-port registry
	// itself; guard them here too so this process's sandboxed commands cannot
	// reach the leader even if that entry is missing (an older leader binary,
	// an unwritable config directory).
	unguardPub := portguard.Register(lockInfo.PubPort, "ipc-primary-pub")
	unguardRPC := portguard.Register(lockInfo.RPCPort, "ipc-primary-rpc")
	unguard := func() { unguardPub(); unguardRPC() }

	logging.Info("IPC: role determined",
		"role", RoleSecondary,
		"workdir", workdir,
		"instance_id", instanceID,
		"pub_port", lockInfo.PubPort,
		"rpc_port", lockInfo.RPCPort,
	)

	ipcClient, clientErr := ipc.NewClient(ctx)
	if clientErr != nil {
		// No client: the follower works normally (the database is local) but
		// cannot watch the leader for failover.
		logging.Warn("IPC bootstrap: failed to create IPC client; failover disabled for this instance", "error", clientErr)
		res.Watcher = failover.NewWatcherForSecondary(failover.DefaultConfig(), instanceID, workdir,
			res.PubPort, res.RPCPort, nil, "", nil)
		res.Watcher.SetEnabled(false)
		res.Cleanup = func() { closeDB(); unguard() }
		return res, nil
	}
	res.IPCClient = ipcClient

	// Use the leader's ports, not the ports derived from the path: when this
	// watcher wins the lock race it writes them into the lock file, and the
	// promotion path binds exactly these ports. Deriving here would publish ports
	// that nobody listens on whenever the running leader bound different ones
	// (an older binary with the previous 40000-60000 port range, or a leader that
	// fell back to FindFreePorts).
	pubAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.PubPort)
	watcher := failover.NewWatcherForSecondary(
		failover.DefaultConfig(),
		instanceID, workdir,
		res.PubPort, res.RPCPort,
		ipcClient,
		pubAddr,
		nil, // the entrypoint sets the promotion callback (App.PromoteToPrimary)
	)
	res.Watcher = watcher
	watcher.Start(ctx)

	res.Cleanup = func() {
		shutdownCtx := context.Background()
		watcher.Shutdown(shutdownCtx)
		_ = ipcClient.Close()
		closeDB()
		unguard()
	}
	return res, nil
}
