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
	// ErrCodeBusy identifies a transient SQLITE_BUSY/SQLITE_LOCKED failure:
	// the write could not acquire the lock in time (e.g. another writer holds
	// it, or a read->write upgrade collided with a concurrent commit — see
	// [[pando/analysis/session_index_locked_residual_risk.md]] section 2).
	// Unlike ErrCodeInternal, this is always safe to retry for an idempotent
	// write such as ReplaceSessionEvents.
	ErrCodeBusy     WriteErrorCode = "BUSY"
	ErrCodeInternal WriteErrorCode = "INTERNAL"
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
	case ErrCodeTimeout, ErrCodeUnreachable, ErrCodeBusy:
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

	// The primary does not recognise this write method at all — most likely a
	// version-skew case: a secondary running newer code (e.g. it knows about
	// EventStore.ReplaceMessageEvents) talking to an older primary binary
	// whose dispatchWrite/RemembrancesWriteDispatcher switch predates that
	// method. Both dispatch layers use a distinct literal message for this:
	// dispatchWrite's own default case ("unknown write method %q", handled
	// via the typed ErrCodeMethodNotFound WriteError already, so it never
	// reaches mapToWriteError as plain text) and
	// internal/rag/proxy.RemembrancesWriteDispatcher's default case
	// ("unsupported remembrances write method %q", a plain fmt.Errorf that
	// does round-trip through mapToWriteError once it crosses the IPC
	// boundary as message-only text). Recognising both here lets a caller use
	// IsMethodNotSupportedError to detect version skew and fall back to an
	// older, more broadly supported write path — see the incremental session
	// indexer's fallback to the whole-transcript ReplaceSessionEvents in
	// internal/app/remembrances_indexer.go.
	case strings.Contains(msg, "unsupported remembrances write method"),
		strings.Contains(msg, "unknown write method"):
		return &WriteError{Code: ErrCodeMethodNotFound, Method: method, Message: msg}

	// SQLITE_BUSY / SQLITE_LOCKED: the write could not acquire the lock in
	// time. isLockError (proxy.go) recognises this both as a typed
	// *sqlite3.Error (a direct local failure, e.g. the primary's own
	// ReplaceSessionEvents on a BeginTx it opened itself) and, via a string
	// match, when the error arrives as a plain-text IPC RPC error: the
	// primary's remembrances dispatcher (internal/rag/proxy.Dispatcher)
	// returns the raw store error without going through mapToWriteError, the
	// JSON-RPC layer flattens it to a message-only rpcError, and the
	// secondary's ipc.Client.Call re-wraps that as an
	// "ipc: RPC error ...: ... database is locked" string with no
	// *sqlite3.Error left to type-assert on.
	case isLockError(err):
		return &WriteError{Code: ErrCodeBusy, Method: method, Message: msg}

	// SQLite unique-constraint violations surface as "UNIQUE constraint failed".
	case strings.Contains(msg, "UNIQUE constraint failed"),
		strings.Contains(msg, "duplicate key"):
		return &WriteError{Code: ErrCodeConflict, Method: method, Message: msg}

	default:
		return &WriteError{Code: ErrCodeInternal, Method: method, Message: msg}
	}
}

// IsBusyOrLockedError reports whether err represents a transient SQLite
// BUSY/LOCKED condition, whether observed directly — e.g. a *sqlite3.Error
// from a local BeginTx that never passes through mapToWriteError — or after
// round-tripping through the IPC write channel, where it surfaces as a
// *WriteError with ErrCodeBusy. Callers that want to retry an idempotent
// write (e.g. the session indexer's ReplaceSessionEvents) should use this
// instead of re-deriving the detection logic themselves.
func IsBusyOrLockedError(err error) bool {
	if err == nil {
		return false
	}
	var werr *WriteError
	if errors.As(err, &werr) {
		return werr.Code == ErrCodeBusy
	}
	return isLockError(err)
}

// IsMethodNotSupportedError reports whether err indicates that the primary
// does not recognise a proxied write method — a version-skew signal (see the
// mapToWriteError case above for the two shapes this can arrive in). Callers
// on a secondary should treat this as "the primary predates this write path"
// and fall back to an older, more broadly supported one rather than treating
// it as a permanent failure of the specific write attempted. Unlike
// IsBusyOrLockedError, this only ever matches after the error has round-
// tripped through mapToWriteError into a *WriteError — a primary (no proxy
// configured) never calls a write method by name over IPC, so this check is
// meaningless, and never true, for a primary's own direct calls.
func IsMethodNotSupportedError(err error) bool {
	if err == nil {
		return false
	}
	var werr *WriteError
	if errors.As(err, &werr) {
		return werr.Code == ErrCodeMethodNotFound
	}
	return false
}
