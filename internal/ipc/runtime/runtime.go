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
	"path/filepath"
	"sync"
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

	// SQLDB is the underlying *sql.DB. Primary holds a RW pool with WAL
	// pragmas applied and migrations run. Secondary holds a 1-connection RW
	// pool with a short busy_timeout (db.ConnectRWSecondary); on failover
	// promotion that same pool is upgraded in place (db.PromoteToPrimaryPool).
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

	// Watcher monitors primary liveness. Non-nil on a primary and on a
	// secondary with a working IPC client. Automatic failover is enabled by
	// default (failover.DefaultConfig); Watcher.SetEnabled(false) turns it off.
	// A secondary watcher never takes the IPC lock until a promotion callback
	// is registered with Watcher.SetPromoteCallback.
	Watcher *failover.Watcher

	// Cleanup releases all resources acquired during Bootstrap. On a primary
	// it follows the ordered handover: release the lock, announce
	// instance.shutdown, close the bus, then close the DB. It is idempotent;
	// the caller should call it on shutdown (typically deferred).
	Cleanup func()

	releaseOnce sync.Once
}

// ReleaseLock releases the primary's IPC lock now, ahead of Cleanup, so a
// shutting-down primary can hand the lock over before it finishes its own
// (possibly slow) shutdown. Idempotent, and a no-op on a secondary; Cleanup
// calls it too.
func (r *BootstrapResult) ReleaseLock() {
	r.releaseOnce.Do(func() {
		if r.LockFile != nil {
			ipc.ReleaseLock(r.LockFile)
		}
	})
}

// NewPrimaryBus creates the Bus a primary serves on, with the ipc.ping
// liveness handler registered, so a freshly started secondary can tell a
// healthy primary apart from a suspended one (see killStalePrimary). Used by
// Bootstrap and by failover promotion, so a promoted primary answers the probe
// exactly like one that started as primary.
func NewPrimaryBus(instanceID string) *ipc.Bus {
	bus := ipc.NewBus(instanceID)
	bus.RegisterMethod(pingMethod, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"pong"`), nil
	})
	return bus
}

// canonicalWorkdir maps every spelling of a working directory (relative,
// absolute, through a symlink) to one path, so they all derive the same
// deterministic ports (ipc.PortsForPath hashes the raw string) and name the
// same lock file. Falls back to the absolute path, then to the input, when
// resolution fails (e.g. a component was removed).
func canonicalWorkdir(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return workdir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// Options customizes Bootstrap's stale-primary handling. Use DefaultOptions
// (what Bootstrap itself uses) unless an entrypoint needs a different policy —
// see BootstrapWithOptions.
type Options struct {
	// ProbeTimeout bounds how long a freshly started secondary waits for the
	// existing primary to answer ipc.ping before treating it as unresponsive.
	// Zero falls back to stalePrimaryProbeTimeout (10s).
	ProbeTimeout time.Duration

	// AllowKillStalePrimary, when true, SIGKILLs an unresponsive primary so
	// this instance can take over — Bootstrap's long-standing behaviour, and
	// correct for a TUI/ACP/serve/desktop/app instance where the previous
	// occupant of this workdir is presumed to be another one of the same kind.
	//
	// When false, an unresponsive primary is left alone: this instance
	// continues as a (degraded) secondary instead. Direct-first sqlc writes
	// still work as normal (DBProxy always tries them locally before
	// forwarding), but every remembrances write (which always proxies, never
	// direct) fails loudly once its forward times out, because there is
	// nobody alive to answer it. Intended for short-lived, low-trust
	// entrypoints (P2's ephemeral mcp-server) that must never kill a user's
	// long-running TUI/desktop/serve instance just because it is slow, under a
	// debugger, or SIGSTOPped to answer one probe.
	AllowKillStalePrimary bool
}

// DefaultOptions returns Bootstrap's long-standing policy: the 10s probe
// timeout (stalePrimaryProbeTimeout) and permission to kill an unresponsive
// primary.
func DefaultOptions() Options {
	return Options{
		ProbeTimeout:          stalePrimaryProbeTimeout,
		AllowKillStalePrimary: true,
	}
}

// Bootstrap runs the unified startup sequence for the given workdir using
// DefaultOptions. See BootstrapWithOptions for the full sequence and for
// entrypoints that need a different stale-primary policy.
func Bootstrap(ctx context.Context, workdir, instanceID string) (*BootstrapResult, error) {
	return BootstrapWithOptions(ctx, workdir, instanceID, DefaultOptions())
}

// BootstrapWithOptions runs the unified startup sequence for the given
// workdir.
//
//  1. Derive deterministic PUB/RPC ports from the path.
//  2. Attempt to acquire the exclusive IPC lock.
//  3. Primary: open RW DB (with migrations), create Bus, create direct Querier.
//  4. Secondary: open RO DB, create IPC Client, create DBProxy Querier.
//
// On lock error the function continues as primary so the caller does not lose
// functionality — consistent with the existing root.go behaviour.
//
// opts controls the stale-primary probe timeout and whether an unresponsive
// primary is killed (see Options). A zero opts.ProbeTimeout falls back to
// stalePrimaryProbeTimeout.
func BootstrapWithOptions(ctx context.Context, workdir, instanceID string, opts Options) (*BootstrapResult, error) {
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = stalePrimaryProbeTimeout
	}
	workdir = canonicalWorkdir(workdir)
	pubPort, rpcPort := ipc.PortsForPath(workdir)

	isPrimary, lockInfo, lockFile, lockErr := ipc.AcquireLock(workdir, instanceID, pubPort, rpcPort)
	if lockErr != nil {
		logging.Warn("IPC lock acquisition failed, continuing as primary without IPC", "error", lockErr)
	}

	// We acquired the secondary role, which means another process holds the lock.
	// That process may, however, be suspended (SIGSTOP) or hung while still
	// holding the flock — in which case this secondary would block forever on the
	// DB proxy. Probe the primary; if it does not answer within the timeout,
	// either kill it and re-acquire the lock so we become the primary instead of
	// hanging (opts.AllowKillStalePrimary), or continue as a degraded secondary
	// without touching it.
	if !isPrimary && lockErr == nil && lockInfo != nil {
		if opts.AllowKillStalePrimary {
			if killStalePrimary(ctx, workdir, lockInfo, opts.ProbeTimeout) {
				isPrimary, lockInfo, lockFile, lockErr = reacquireAfterKill(workdir, instanceID, pubPort, rpcPort)
			}
		} else if lockInfo.PID > 0 && lockInfo.PID != os.Getpid() {
			rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.RPCPort)
			if !primaryResponds(ctx, rpcAddr, opts.ProbeTimeout) {
				logging.Warn("IPC: primary did not respond within the probe timeout; continuing as a degraded secondary instead of killing it",
					"workdir", workdir,
					"primary_pid", lockInfo.PID,
					"primary_instance", lockInfo.InstanceID,
					"timeout", opts.ProbeTimeout,
				)
			}
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

		bus := NewPrimaryBus(instanceID)

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

		res.Cleanup = sync.OnceFunc(func() {
			// Ordered handover: release the lock BEFORE announcing
			// instance.shutdown, so a secondary reacting to the announcement
			// finds the lock free. Bus.Shutdown announces and then closes the
			// sockets; the watcher stops afterwards (its own shutdown publish
			// then hits a closed bus and is skipped). All steps are idempotent:
			// app.App's primary handover may already have done the first two.
			res.ReleaseLock()
			_ = bus.Shutdown()
			watcher.Shutdown(context.Background())
			_ = conn.Close()
		})

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
		// Every entrypoint builds its App on res.SQLDB, and app.New panics on a
		// nil pool. A secondary without a database cannot do anything useful,
		// so fail the bootstrap with the reason instead of returning a result
		// every caller would have to remember to check.
		logging.Error("IPC bootstrap: failed to open the secondary database", "error", rwErr)
		return nil, fmt.Errorf("ipc/runtime: open secondary DB: %w", rwErr)
	}

	logging.Debug("IPC: secondary RW DB opened (WAL mode)", "role", RoleSecondary, "workdir", workdir)

	ipcClient, clientErr := ipc.NewClient(ctx)
	if clientErr != nil {
		// Keep the pool open: it is the direct Querier returned below, and the
		// cleanup closes it. (It used to be closed here, which handed callers a
		// closed *sql.DB.)
		logging.Warn("IPC bootstrap: failed to create IPC client, secondary has no proxy", "error", clientErr)
		res.SQLDB = rwConn
		res.Querier = db.New(rwConn)
		res.Cleanup = sync.OnceFunc(func() { _ = rwConn.Close() })
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

	// Create a failover watcher for this secondary. Auto-failover is enabled by
	// default (failover.DefaultConfig), but the watcher is created WITHOUT a
	// promotion callback: until the entrypoint registers one with
	// Watcher.SetPromoteCallback (TUI and ACP do; serve/desktop/app do not yet),
	// it only monitors and never takes the IPC lock, so it cannot leave a
	// zombie primary behind.
	//
	// It is bound to the primary's ports from the lock file (not the ports
	// derived from this workdir): if this instance wins a promotion it binds
	// and records those same ports, so every other secondary's DBProxy — which
	// points at them — keeps working, even against an older primary that
	// derived its ports from a differently spelled path.
	watcherPubPort, watcherRPCPort := lockInfo.PubPort, lockInfo.RPCPort
	if watcherPubPort == 0 || watcherRPCPort == 0 {
		watcherPubPort, watcherRPCPort = pubPort, rpcPort
	}
	watcher := failover.NewWatcherForSecondary(
		failover.DefaultConfig(),
		instanceID, workdir,
		watcherPubPort, watcherRPCPort,
		ipcClient,
		pubAddr,
		nil,
	)
	res.Watcher = watcher
	// Start the secondary watcher immediately; it monitors primary heartbeats
	// and, once a promotion callback is registered, promotes this instance if
	// the primary dies.
	watcher.Start(ctx)

	res.Cleanup = sync.OnceFunc(func() {
		watcher.Shutdown(context.Background())
		_ = ipcClient.Close()
		_ = rwConn.Close()
	})

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
// respond to pingMethod within timeout (e.g. it is hung while still holding
// the flock), the primary process is killed so this instance can take over.
// Returns true when the primary was killed and the lock should be re-acquired.
//
// A primary that is merely SUSPENDED — stopped by job control (SIGSTOP/^Z) or
// halted in a debugger — is never killed (G7 of
// pando/plans/mcp_server_ipc_bootstrap.md). Such a process cannot answer the
// probe by definition, yet it is perfectly healthy and its user expects to
// resume it: SIGKILLing a TUI someone paused under a debugger, or ^Z'd in
// their shell, destroys real work. It keeps holding the flock, so this
// instance continues as a secondary and the user is told to resume or kill it
// themselves. Only genuinely unresponsive-but-running (R/S/D), zombie, or
// already-gone primaries are killed. Off Linux the state cannot be read
// (procStateSupported is false) and the long-standing kill behaviour is kept.
func killStalePrimary(ctx context.Context, workdir string, lockInfo *ipc.LockInfo, timeout time.Duration) bool {
	if lockInfo == nil || lockInfo.PID <= 0 || lockInfo.PID == os.Getpid() {
		return false
	}

	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", lockInfo.RPCPort)
	if primaryResponds(ctx, rpcAddr, timeout) {
		return false
	}

	if state, ok := processState(lockInfo.PID); ok && isSuspendedState(state) {
		logging.Warn("IPC: primary is suspended, not killing it; this instance continues as a secondary",
			"workdir", workdir,
			"primary_pid", lockInfo.PID,
			"primary_instance", lockInfo.InstanceID,
			"process_state", string(state),
			"reason", suspendedStateReason(state),
			"action", fmt.Sprintf("resume it with `kill -CONT %d` (or continue it in your debugger), or stop it yourself with `kill %d`", lockInfo.PID, lockInfo.PID),
		)
		return false
	}

	logging.Warn("IPC: primary did not respond within timeout, killing it to take over",
		"workdir", workdir,
		"primary_pid", lockInfo.PID,
		"primary_instance", lockInfo.InstanceID,
		"timeout", timeout,
	)

	if err := killProcess(lockInfo.PID); err != nil {
		logging.Warn("IPC: failed to kill unresponsive primary", "pid", lockInfo.PID, "error", err)
		return false
	}

	// Wait for the killed process to fully exit so the kernel releases the flock.
	waitForProcessExit(lockInfo.PID, 2*time.Second)
	return true
}

// isSuspendedState reports whether a Linux process state letter means the
// process is stopped rather than unresponsive: 'T' is stopped by a job-control
// signal (SIGSTOP/SIGTSTP, `kill -STOP`, shell ^Z) and 't' is stopped in a
// ptrace trap (a debugger). Both resume on SIGCONT/continue and are healthy.
func isSuspendedState(state byte) bool {
	return state == 'T' || state == 't'
}

// suspendedStateReason renders a suspended state letter for the log line that
// tells the user why their primary was left alone.
func suspendedStateReason(state byte) string {
	if state == 't' {
		return "stopped by a debugger (ptrace)"
	}
	return "stopped by a job-control signal (SIGSTOP/SIGTSTP)"
}

// primaryResponds reports whether the primary at rpcAddr answers an RPC within
// timeout. Any reply — including a "method not found" error from an older
// primary that lacks the ping handler — counts as alive; only a timeout (or
// inability to reach the RPC loop at all) counts as unresponsive.
func primaryResponds(ctx context.Context, rpcAddr string, timeout time.Duration) bool {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
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
