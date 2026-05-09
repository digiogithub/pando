// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"hash/fnv"
	"net"
)

const (
	portBase  = 40000
	portRange = 20000
)

// PortsForPath returns the deterministic PUB and RPC ports for a given absolute path.
// The ports are derived from a FNV-32a hash of the path:
//
//	base_port = 40000 + (fnv32a(abs_path) % 20000)
//	PUB port  = base_port
//	RPC port  = base_port + 1
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
