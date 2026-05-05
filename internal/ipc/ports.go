// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"hash/fnv"
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
