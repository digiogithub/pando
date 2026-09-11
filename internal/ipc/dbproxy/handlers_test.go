// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	sqlite3 "github.com/ncruces/go-sqlite3"
	sqlite3driver "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
)

type busRecorder struct {
	handler ipc.HandlerFunc
}

func (b *busRecorder) RegisterMethod(_ string, handler ipc.HandlerFunc) {
	b.handler = handler
}

type recordingQuerier struct {
	db.Querier
	createSessionCalled bool
	createSessionArg    db.CreateSessionParams
	createSessionResult db.Session
	createSessionErr    error
}

func (r *recordingQuerier) CreateSession(ctx context.Context, arg db.CreateSessionParams) (db.Session, error) {
	r.createSessionCalled = true
	r.createSessionArg = arg
	if r.createSessionErr != nil {
		return db.Session{}, r.createSessionErr
	}
	return r.createSessionResult, nil
}

func invokeCreateSessionHandler(handler ipc.HandlerFunc) (json.RawMessage, error) {
	if handler == nil {
		return nil, errors.New("expected db.write handler to be registered")
	}
	payload, err := json.Marshal(db.CreateSessionParams{ID: "sess-123", Title: "non-interactive"})
	if err != nil {
		return nil, err
	}
	request, err := json.Marshal(WriteRequest{Method: "CreateSession", Params: payload})
	if err != nil {
		return nil, err
	}
	return handler(context.Background(), MethodDBWrite, request)
}

func TestRegisterHandlers_RegistersDBWriteBeforeBusStart(t *testing.T) {
	bus := &busRecorder{}
	querier := &recordingQuerier{
		createSessionResult: db.Session{ID: "sess-123", Title: "non-interactive"},
	}

	RegisterHandlers(bus, querier)

	raw, err := invokeCreateSessionHandler(bus.handler)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !querier.createSessionCalled {
		t.Fatal("expected CreateSession to be dispatched")
	}
	if querier.createSessionArg.Title != "non-interactive" {
		t.Fatalf("expected CreateSession title to round-trip, got %q", querier.createSessionArg.Title)
	}

	var got db.Session
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.ID != querier.createSessionResult.ID {
		t.Fatalf("expected session ID %q, got %q", querier.createSessionResult.ID, got.ID)
	}
}

type submitterRecorder struct {
	called bool
	result json.RawMessage
	err    error
}

func (s *submitterRecorder) Submit(_ context.Context, req WriteRequest) (json.RawMessage, error) {
	s.called = true
	if req.Method != "CreateSession" {
		return nil, errors.New("unexpected method: " + req.Method)
	}
	return s.result, s.err
}

func TestRegisterHandlersWithCoordinator_RegistersDBWriteBeforeBusStart(t *testing.T) {
	bus := &busRecorder{}
	submitter := &submitterRecorder{result: json.RawMessage(`{"id":"sess-123","title":"non-interactive"}`)}

	RegisterHandlersWithCoordinator(bus, submitter)

	raw, err := invokeCreateSessionHandler(bus.handler)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !submitter.called {
		t.Fatal("expected CreateSession to be dispatched through coordinator")
	}

	var got db.Session
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.ID != "sess-123" {
		t.Fatalf("expected session ID %q, got %q", "sess-123", got.ID)
	}
}

func TestDispatchWrite_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	req := WriteRequest{Method: "NonExistent", Params: json.RawMessage(`{}`)}
	_, err := dispatchWrite(context.Background(), &recordingQuerier{}, req)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	var werr *WriteError
	if !errors.As(err, &werr) {
		t.Fatalf("expected *WriteError, got %T: %v", err, err)
	}
	if werr.Code != ErrCodeMethodNotFound {
		t.Fatalf("expected ErrCodeMethodNotFound, got %s", werr.Code)
	}
}

func TestDispatchWrite_BadParamsReturnsInvalidParams(t *testing.T) {
	// Pass invalid JSON for CreateSession params.
	req := WriteRequest{Method: "CreateSession", Params: json.RawMessage(`not-json`)}
	_, err := dispatchWrite(context.Background(), &recordingQuerier{}, req)
	if err == nil {
		t.Fatal("expected error for invalid params")
	}
	var werr *WriteError
	if !errors.As(err, &werr) {
		t.Fatalf("expected *WriteError, got %T: %v", err, err)
	}
	if werr.Code != ErrCodeInvalidParams {
		t.Fatalf("expected ErrCodeInvalidParams, got %s", werr.Code)
	}
}

func TestWriteError_IsRetryable(t *testing.T) {
	retryable := []WriteErrorCode{ErrCodeTimeout, ErrCodeUnreachable, ErrCodeBusy}
	for _, code := range retryable {
		werr := &WriteError{Code: code}
		if !werr.IsRetryable() {
			t.Errorf("expected %s to be retryable", code)
		}
	}

	nonRetryable := []WriteErrorCode{ErrCodeMethodNotFound, ErrCodeInvalidParams, ErrCodeConflict, ErrCodeInternal}
	for _, code := range nonRetryable {
		werr := &WriteError{Code: code}
		if werr.IsRetryable() {
			t.Errorf("expected %s to NOT be retryable", code)
		}
	}
}

// openBusyReproDB opens a temp-file SQLite DB configured exactly like the
// production pool (BEGIN IMMEDIATE writes, a short busy_timeout) so a second
// writer's BeginTx reliably fails with a genuine SQLITE_BUSY error — the same
// failure mode the primary's own indexer hits when it calls
// events.ReplaceSessionEvents directly (no IPC round trip involved).
func openBusyReproDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "busy-repro.db")
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=busy_timeout(50)", path)
	sqlDB, err := sqlite3driver.Open(dsn)
	if err != nil {
		t.Fatalf("open busy-repro DB: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE t(x INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return sqlDB
}

func TestMapToWriteError_BusyAndLockedErrorsAreErrCodeBusy(t *testing.T) {
	sqlDB := openBusyReproDB(t)
	ctx := context.Background()

	tx1, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx #1: %v", err)
	}
	defer tx1.Rollback() //nolint:errcheck

	_, busyErr := sqlDB.BeginTx(ctx, nil)
	if busyErr == nil {
		t.Fatal("BeginTx #2 succeeded while #1 held the write lock open; want SQLITE_BUSY")
	}
	// The ncruces driver does not always wrap this as *sqlite3.Error — a bare
	// BeginTx collision surfaces as a plain sqlite3.ExtendedErrorCode value
	// instead — so assert via errors.Is (which isLockError also now uses)
	// rather than assuming a concrete type.
	if !errors.Is(busyErr, sqlite3.BUSY) {
		t.Fatalf("expected errors.Is(err, sqlite3.BUSY), got %T: %v", busyErr, busyErr)
	}

	cases := []struct {
		name string
		err  error
	}{
		{"genuine SQLITE_BUSY from a real BeginTx collision", busyErr},
		{"plain database is locked string (primary's own direct wrap)", errors.New("events: fts delete: sqlite3: database is locked")},
		{"IPC RPC error string wrapping a lock failure (secondary side)", fmt.Errorf("ipc: RPC error -32000: %s", "events: fts delete: sqlite3: database is locked")},
		{"database table is locked variant", errors.New("sqlite3: database table is locked")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			werr := mapToWriteError("ReplaceSessionEvents", tc.err)
			if werr.Code != ErrCodeBusy {
				t.Fatalf("mapToWriteError(%v) code = %s, want %s", tc.err, werr.Code, ErrCodeBusy)
			}
			if !werr.IsRetryable() {
				t.Fatalf("mapToWriteError(%v) should be retryable", tc.err)
			}
			if !IsBusyOrLockedError(tc.err) {
				t.Fatalf("IsBusyOrLockedError(%v) = false, want true", tc.err)
			}
			// And once it has round-tripped through mapToWriteError into a
			// *WriteError (simulating the secondary side re-deriving the code
			// after an IPC round trip), IsBusyOrLockedError must still say yes.
			if !IsBusyOrLockedError(werr) {
				t.Fatalf("IsBusyOrLockedError(%v) = false, want true", werr)
			}
		})
	}
}

// TestMapToWriteError_UnsupportedMethodTextIsErrCodeMethodNotFound covers the
// version-skew detection path (see [[pando/features/session_index_incremental_per_message.md]]):
// a secondary running newer code (e.g. it knows about
// events.EventStore.ReplaceMessageEvents) can talk to an older primary
// binary whose write dispatcher does not recognise that method name yet.
// Both dispatch layers surface that as a distinct plain-text message —
// dispatchWrite's own "unknown write method %q" (though that one is already
// wrapped as a typed ErrCodeMethodNotFound WriteError before it ever reaches
// mapToWriteError as plain text — included here for completeness) and
// internal/rag/proxy.RemembrancesWriteDispatcher's "unsupported remembrances
// write method %q" (a plain fmt.Errorf that does round-trip through
// mapToWriteError once it crosses the IPC boundary as message-only text,
// exactly as exercised by
// TestDispatchRemembrancesWrite_UnsupportedMethodMessageMatchesVersionSkewDetection
// in internal/rag/proxy) — this test pins that mapToWriteError classifies
// both, is not retryable, and IsMethodNotSupportedError recognises the
// result.
func TestMapToWriteError_UnsupportedMethodTextIsErrCodeMethodNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"rag dispatcher default case, direct", fmt.Errorf("rag dispatcher: unsupported remembrances write method %q", "ReplaceMessageEvents")},
		{"rag dispatcher default case, round-tripped through IPC", fmt.Errorf("ipc: RPC error -32000: %s", `rag dispatcher: unsupported remembrances write method "ReplaceMessageEvents"`)},
		{"dispatchWrite default case text", errors.New(`unknown write method "SomeFutureMethod"`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			werr := mapToWriteError("ReplaceMessageEvents", tc.err)
			if werr.Code != ErrCodeMethodNotFound {
				t.Fatalf("mapToWriteError(%v) code = %s, want %s", tc.err, werr.Code, ErrCodeMethodNotFound)
			}
			if werr.IsRetryable() {
				t.Fatalf("mapToWriteError(%v) should not be retryable (version skew is not transient)", tc.err)
			}
			// IsMethodNotSupportedError, unlike IsBusyOrLockedError, only
			// ever matches after an error has round-tripped through
			// mapToWriteError into a *WriteError (see its doc comment) — the
			// only shape a caller like the incremental indexer actually
			// receives, since every real call site that can hit this
			// (dbproxy.DBProxy.WriteWithRetry on a secondary) already maps
			// the error before returning it.
			if !IsMethodNotSupportedError(werr) {
				t.Fatalf("IsMethodNotSupportedError(%v) = false, want true (after round-tripping into *WriteError)", werr)
			}
		})
	}
}

func TestIsMethodNotSupportedError_NonMatchingErrorsAreFalse(t *testing.T) {
	cases := []error{
		nil,
		errors.New("boom"),
		&WriteError{Code: ErrCodeInternal, Message: "boom"},
		&WriteError{Code: ErrCodeBusy, Message: "database is locked"},
	}
	for _, err := range cases {
		if IsMethodNotSupportedError(err) {
			t.Errorf("IsMethodNotSupportedError(%v) = true, want false", err)
		}
	}
}

func TestIsBusyOrLockedError_NonBusyErrorsAreFalse(t *testing.T) {
	cases := []error{
		nil,
		errors.New("boom"),
		&WriteError{Code: ErrCodeInternal, Message: "boom"},
		&WriteError{Code: ErrCodeConflict, Message: "UNIQUE constraint failed: sessions.id"},
	}
	for _, err := range cases {
		if IsBusyOrLockedError(err) {
			t.Errorf("IsBusyOrLockedError(%v) = true, want false", err)
		}
	}
}

func TestRegisterHandlers_PropagatesQuerierErrors(t *testing.T) {
	bus := &busRecorder{}
	querier := &recordingQuerier{createSessionErr: errors.New("boom")}

	RegisterHandlers(bus, querier)

	payload, err := json.Marshal(db.CreateSessionParams{ID: "sess-123", Title: "non-interactive"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	request, err := json.Marshal(WriteRequest{Method: "CreateSession", Params: payload})
	if err != nil {
		t.Fatalf("marshal write request: %v", err)
	}

	handler := bus.handler
	if handler == nil {
		t.Fatal("expected db.write handler to be registered")
	}

	_, err = handler(context.Background(), MethodDBWrite, request)
	if err == nil {
		t.Fatal("expected handler to return error")
	}
	// DB errors are now wrapped as *WriteError; verify the original message is preserved.
	var werr *WriteError
	if !errors.As(err, &werr) {
		t.Fatalf("expected *WriteError, got %T: %v", err, err)
	}
	if werr.Code != ErrCodeInternal {
		t.Fatalf("expected ErrCodeInternal, got %s", werr.Code)
	}
	if !strings.Contains(werr.Message, "boom") {
		t.Fatalf("expected original message in WriteError, got %q", werr.Message)
	}
}
