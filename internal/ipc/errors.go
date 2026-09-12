// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import "errors"

var (
	// ErrPrimaryExists is returned when a primary instance already holds the lock.
	ErrPrimaryExists = errors.New("ipc: primary instance already running")

	// ErrPrimaryLockHeld is returned by AcquireLock when another process
	// demonstrably holds the IPC lock but its LockInfo could not be read. The
	// caller must NOT continue as primary: another writer owns the database.
	ErrPrimaryLockHeld = errors.New("ipc: lock held by another instance, but its info is unreadable")

	// ErrNotPrimary is returned when an operation requires being the primary instance.
	ErrNotPrimary = errors.New("ipc: this instance is not the primary")

	// ErrMethodNotFound is returned when a requested RPC method has no handler.
	ErrMethodNotFound = errors.New("ipc: method not found")

	// ErrTimeout is returned when an RPC call times out.
	ErrTimeout = errors.New("ipc: call timeout")

	// ErrConnectionFailed is returned when a connection attempt fails.
	ErrConnectionFailed = errors.New("ipc: connection failed")
)
