// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
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
