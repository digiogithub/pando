// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/ipc"
)

// WriteErrorCode identifies the category of a write-channel failure.
type WriteErrorCode string

const (
	ErrCodeTimeout        WriteErrorCode = "TIMEOUT"
	ErrCodeUnreachable    WriteErrorCode = "UNREACHABLE"
	ErrCodeMethodNotFound WriteErrorCode = "METHOD_NOT_FOUND"
	ErrCodeInvalidParams  WriteErrorCode = "INVALID_PARAMS"
	ErrCodeConflict       WriteErrorCode = "CONFLICT"
	ErrCodeInternal       WriteErrorCode = "INTERNAL"
)

// WriteError is a structured error returned by the write channel.
type WriteError struct {
	Code    WriteErrorCode `json:"code"`
	Message string         `json:"message"`
	Method  string         `json:"method"`
}

func (e *WriteError) Error() string {
	return fmt.Sprintf("dbproxy: %s (%s): %s", e.Code, e.Method, e.Message)
}

// IsRetryable reports whether the error is transient and the operation may succeed on retry.
func (e *WriteError) IsRetryable() bool {
	switch e.Code {
	case ErrCodeTimeout, ErrCodeUnreachable:
		return true
	default:
		return false
	}
}

// mapToWriteError converts a raw error into a *WriteError with an appropriate code.
// It inspects well-known sentinel errors from the ipc package and common SQL patterns
// to choose the most precise code.
func mapToWriteError(method string, err error) *WriteError {
	if err == nil {
		return nil
	}

	msg := err.Error()

	switch {
	case errors.Is(err, ipc.ErrTimeout):
		return &WriteError{Code: ErrCodeTimeout, Method: method, Message: msg}

	case errors.Is(err, ipc.ErrConnectionFailed):
		return &WriteError{Code: ErrCodeUnreachable, Method: method, Message: msg}

	// SQLite unique-constraint violations surface as "UNIQUE constraint failed".
	case strings.Contains(msg, "UNIQUE constraint failed"),
		strings.Contains(msg, "duplicate key"):
		return &WriteError{Code: ErrCodeConflict, Method: method, Message: msg}

	default:
		return &WriteError{Code: ErrCodeInternal, Method: method, Message: msg}
	}
}
