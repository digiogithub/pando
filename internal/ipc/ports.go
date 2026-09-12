// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"hash/fnv"
	"net"
)

const (
	// portBase / portRange define the window the deterministic per-path IPC ports
	// are drawn from: base = portBase + fnv32a(path) % portRange, rpc = base + 1.
	//
	// The window MUST stay clear of the OS ephemeral (dynamic) port ranges,
	// otherwise an unrelated program can transiently own the port and the Pando
	// primary silently fails to bind its bus:
	//
	//	Linux   32768-60999 (net.ipv4.ip_local_port_range default)
	//	macOS   49152-65535
	//	Windows 49152-65535
	//
	// So "above 61000" is only safe on Linux; the window has to sit BELOW 32768.
	// We use 20000-25999 (rpc up to 26000): comfortably above the well-known and
	// commonly registered dev-tooling ports (3000, 3306, 5000, 5432, 6379, 8000,
	// 8080, 9000, 9090) and below the clusters just above it (26257 CockroachDB,
	// 27015 Steam, 27017-27019 MongoDB, 28015 RethinkDB).
	//
	// Historical note: this used to be 40000-60000, which overlaps every
	// ephemeral range above and caused intermittent "continuing without IPC"
	// startups. A bind failure is no longer silent — see StartBusWithRetry.
	portBase  = 20000
	portRange = 6000
)

// PortsForPath returns the deterministic PUB and RPC ports for a given absolute path.
// The ports are derived from a FNV-32a hash of the path:
//
//	base_port = 20000 + (fnv32a(abs_path) % 6000)
//	PUB port  = base_port
//	RPC port  = base_port + 1
//
// Note that only a primary derives its ports this way. A secondary always uses
// the ports recorded in the lock file by the running primary (see ReadLockForPath
// and ipc/runtime.Bootstrap), so an instance of this binary still connects to a
// primary started by an older binary that derived its ports from the old range.
func PortsForPath(absPath string) (pub, rpc int) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(absPath))
	base := portBase + int(h.Sum32()%portRange)
	return base, base + 1
}

// FindFreePorts finds two consecutive free TCP ports. This is useful for
// secondary instances (serve, app, desktop) that cannot bind the deterministic
// ports already held by a primary TUI instance on the same path.
func FindFreePorts() (pub, rpc int, err error) {
	// Request two OS-assigned ports, then close and return the numbers.
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, 0, err
	}
	p1 := l1.Addr().(*net.TCPAddr).Port
	_ = l1.Close()

	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, 0, err
	}
	p2 := l2.Addr().(*net.TCPAddr).Port
	_ = l2.Close()

	return p1, p2, nil
}
