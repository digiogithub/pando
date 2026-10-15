// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

const (
	// busBindRetryFor is how long StartBusWithRetry keeps retrying the bind.
	// It mainly covers a graceful handover, where the outgoing primary still owns
	// its sockets for the short linger after it released the lock.
	busBindRetryFor = 3 * time.Second

	// busBindRetryEvery is the delay between bind attempts.
	busBindRetryEvery = 50 * time.Millisecond
)

// ErrBusBindFailed wraps every failure of StartBusWithRetry so callers can tell
// "the ports are taken" apart from any other startup error.
var ErrBusBindFailed = errors.New("ipc: could not bind the IPC bus ports")

// StartBusWithRetry starts bus on the given ports, retrying for busBindRetryFor
// before giving up. On failure it logs at Error level (never to stdout, which must
// stay clean for the mcp-server stdio transport) with the exact ports and the most
// likely cause, and returns an error wrapping ErrBusBindFailed.
//
// A primary that cannot bind keeps the IPC lock and the read-write database
// connection on purpose: it is still the single legitimate writer for this
// workdir, and releasing the lock would let a second process open the DB
// read-write. The visible consequence is that secondaries cannot reach it over
// RPC; the Error log is what makes that state diagnosable instead of silent.
func StartBusWithRetry(ctx context.Context, bus *Bus, pubPort, rpcPort int) error {
	if bus == nil {
		return fmt.Errorf("%w: nil bus", ErrBusBindFailed)
	}

	deadline := time.Now().Add(busBindRetryFor)
	var lastErr error
	for attempt := 1; ; attempt++ {
		lastErr = bus.Start(ctx, pubPort, rpcPort)
		if lastErr == nil {
			if attempt > 1 {
				logging.Info("IPC: bus bound after retry",
					"attempt", attempt, "pub_port", pubPort, "rpc_port", rpcPort)
			}
			return nil
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(busBindRetryEvery):
		}
	}

	pubHolder := describePortHolder(pubPort)
	rpcHolder := describePortHolder(rpcPort)
	logging.Error("IPC: FAILED to bind the IPC bus ports; this instance is primary but unreachable over IPC",
		"error", lastErr,
		"pub_port", pubPort,
		"rpc_port", rpcPort,
		"pub_port_state", pubHolder,
		"rpc_port_state", rpcHolder,
		"retried_for", busBindRetryFor.String(),
		"likely_cause", "another process owns one of the derived ports",
		"hint", "run `pando ipc status` and check the listeners on these ports (ss -ltnp / lsof -iTCP -sTCP:LISTEN)",
	)
	return fmt.Errorf("%w on pub=%d rpc=%d (pub: %s, rpc: %s): %w",
		ErrBusBindFailed, pubPort, rpcPort, pubHolder, rpcHolder, lastErr)
}

// describePortHolder reports, cheaply and portably, whether the given loopback
// port is currently taken. Identifying the owning PID is deliberately not
// attempted: there is no portable, cheap way to do it and the failure path must
// not fork external tools.
func describePortHolder(port int) string {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		_ = l.Close()
		return "free now (the bind failure is not a plain port conflict)"
	}
	msg := err.Error()
	if strings.Contains(msg, "address already in use") || strings.Contains(msg, "Only one usage") {
		return "in use by another process"
	}
	return "unavailable: " + msg
}

// ClaimLeaderPorts settles the ports a new leader binds its bus on and returns
// them. lockFile is the IPC lock file this process just acquired (AcquireLock
// or a failover promotion) and pubPort/rpcPort are the ports recorded in it.
//
// Those ports are derived from the workdir (or inherited from the previous
// leader), so an unrelated process can own one of them. Followers, `pando ipc
// status` and cross-project delegation all dial the ports recorded in the lock
// file, never the derived ones, so when the recorded ports stay taken for
// busBindRetryFor this falls back to two OS-assigned loopback ports and records
// them in the lock file, instead of leaving the leader unreachable over IPC.
// If that fails, the recorded ports are returned unchanged and
// StartBusWithRetry reports the conflict as before.
func ClaimLeaderPorts(ctx context.Context, lockFile *os.File, pubPort, rpcPort int) (int, int) {
	if lockFile == nil {
		return pubPort, rpcPort
	}

	deadline := time.Now().Add(busBindRetryFor)
	for {
		if portsFree(pubPort, rpcPort) {
			return pubPort, rpcPort
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(busBindRetryEvery):
		}
	}
	if ctx.Err() != nil {
		return pubPort, rpcPort
	}

	freePub, freeRPC, err := FindFreePorts()
	if err != nil {
		logging.Warn("IPC: preferred bus ports are taken and no free ports were found", "error", err)
		return pubPort, rpcPort
	}
	if err := updateLockPorts(lockFile, freePub, freeRPC); err != nil {
		logging.Warn("IPC: cannot record fallback bus ports in the lock file", "error", err)
		return pubPort, rpcPort
	}
	logging.Warn("IPC: preferred bus ports are taken by another process; using OS-assigned ports recorded in the lock file",
		"preferred_pub_port", pubPort,
		"preferred_rpc_port", rpcPort,
		"pub_port_state", describePortHolder(pubPort),
		"rpc_port_state", describePortHolder(rpcPort),
		"pub_port", freePub,
		"rpc_port", freeRPC,
	)
	return freePub, freeRPC
}

// portsFree reports whether both loopback ports can be bound right now.
func portsFree(ports ...int) bool {
	for _, port := range ports {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return false
		}
		_ = l.Close()
	}
	return true
}
