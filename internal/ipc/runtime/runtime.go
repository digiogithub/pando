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
	"fmt"
	"os"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/changepub"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/failover"
	"github.com/digiogithub/pando/internal/logging"
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

	roConn, roErr := db.ConnectReadOnly()
	if roErr != nil {
		// Cannot open RO DB — fall back gracefully with an empty cleanup.
		logging.Warn("IPC bootstrap: failed to open read-only DB, secondary has no DB", "error", roErr)
		res.Cleanup = func() {}
		return res, nil
	}

	logging.Debug("IPC: secondary RO DB opened", "role", RoleSecondary, "workdir", workdir)

	ipcClient, clientErr := ipc.NewClient(ctx)
	if clientErr != nil {
		_ = roConn.Close()
		logging.Warn("IPC bootstrap: failed to create IPC client, secondary has no proxy", "error", clientErr)
		res.SQLDB = roConn
		res.Querier = db.New(roConn)
		res.Cleanup = func() { _ = roConn.Close() }
		return res, nil
	}

	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.RPCPort)
	proxy := dbproxy.New(db.New(roConn), ipcClient, rpcAddr)

	res.SQLDB = roConn
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
		_ = roConn.Close()
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
