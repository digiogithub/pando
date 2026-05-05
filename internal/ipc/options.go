// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import "time"

// Options configures the IPC package behaviour.
type Options struct {
	// DialTimeout is the max time to wait when connecting. Default: 5s.
	DialTimeout time.Duration
	// CallTimeout is the max time to wait for an RPC response. Default: 10s.
	CallTimeout time.Duration
}

// DefaultOptions holds the default IPC options.
var DefaultOptions = Options{
	DialTimeout: 5 * time.Second,
	CallTimeout: 10 * time.Second,
}
