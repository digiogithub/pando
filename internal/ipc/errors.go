// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import "errors"

var (
	// ErrPrimaryExists is returned when a primary instance already holds the lock.
	ErrPrimaryExists = errors.New("ipc: primary instance already running")

	// ErrNotPrimary is returned when an operation requires being the primary instance.
	ErrNotPrimary = errors.New("ipc: this instance is not the primary")

	// ErrMethodNotFound is returned when a requested RPC method has no handler.
	ErrMethodNotFound = errors.New("ipc: method not found")

	// ErrTimeout is returned when an RPC call times out.
	ErrTimeout = errors.New("ipc: call timeout")

	// ErrConnectionFailed is returned when a connection attempt fails, or when
	// a request could not be sent: in both cases the peer never received it.
	ErrConnectionFailed = errors.New("ipc: connection failed")

	// ErrResponseLost is returned when a request was sent but the connection
	// failed before its response arrived. Like ErrTimeout, the outcome is
	// unknown: the peer may or may not have processed the request.
	ErrResponseLost = errors.New("ipc: response lost")
)

// classifiedError tags err with a sentinel (errors.Is matches both) while
// keeping err's message unchanged, so callers matching on the text see
// exactly what they saw before the tag was added.
type classifiedError struct {
	sentinel error
	err      error
}

func (e *classifiedError) Error() string   { return e.err.Error() }
func (e *classifiedError) Unwrap() []error { return []error{e.sentinel, e.err} }

func classify(sentinel, err error) error {
	return &classifiedError{sentinel: sentinel, err: err}
}
