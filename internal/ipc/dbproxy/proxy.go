// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package dbproxy provides a db.Querier implementation that transparently
// routes write operations while keeping reads fast.
//
// Write strategy (secondary instances):
//  1. Attempt the write directly against the local RW SQLite connection (WAL
//     mode allows concurrent writes when there is no lock contention).
//  2. If SQLite returns BUSY or LOCKED (another writer holds the lock) the
//     operation is forwarded to the primary instance via ZMQ JSON-RPC, which
//     serialises writes as the single authoritative writer.
//  3. Only if the proxy call also fails is an error returned to the caller.
//
// Primary instances use the embedded Querier directly; no proxying occurs.
package dbproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	sqlite3 "github.com/ncruces/go-sqlite3"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/logging"
)

// MethodDBWrite is the JSON-RPC method name for proxied write operations.
const MethodDBWrite = "db.write"

// WriteMeta carries tracing metadata attached to every proxied write request.
// The primary logs this on each write, making write provenance easy to trace.
type WriteMeta struct {
	SourceInstanceID string `json:"source_instance_id"`
	RequestID        string `json:"request_id"`
	Timestamp        string `json:"timestamp"` // RFC3339
}

// WriteRequest is the JSON-RPC params struct for a proxied write.
type WriteRequest struct {
	Meta   WriteMeta       `json:"meta"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// WriteTimeout groups deadline durations for different write categories.
type WriteTimeout struct {
	Default time.Duration
	Long    time.Duration
}

// DefaultWriteTimeouts is used when no explicit timeout is provided.
var DefaultWriteTimeouts = WriteTimeout{
	Default: 5 * time.Second,
	Long:    30 * time.Second,
}

// DBProxy implements db.Querier. Reads are served from the embedded local
// querier. Writes first attempt the local RW connection (WAL mode); on
// SQLITE_BUSY/LOCKED they are forwarded via ZMQ JSON-RPC to the primary.
//
// When client is nil the proxy behaves identically to the embedded querier
// (useful for the primary instance itself, which never proxies).
//
// The client is held in an atomic pointer because failover promotion turns a
// live secondary's proxy into a passthrough (Promote) while other goroutines
// keep writing through it: every write reads the pointer exactly once and acts
// on that snapshot.
type DBProxy struct {
	db.Querier // local reads and direct write attempts — embedded interface
	client     atomic.Pointer[ipc.Client]
	// rpcAddr is the primary's ROUTER endpoint. It changes only when the
	// resolver reports a different primary after a failover (normally the
	// ports are identical: every instance derives them from the canonical
	// workdir, and a promoted instance rebinds the lock-file ports).
	rpcAddr    atomic.Pointer[string]
	instanceID string
	// resolver re-reads where the current primary serves (the IPC lock
	// file); consulted between forwarding retries. Optional.
	resolver atomic.Pointer[PrimaryResolver]
	// handoverWait overrides DefaultHandoverWait when > 0 (nanoseconds).
	handoverWait atomic.Int64
}

// PrimaryResolver reports the RPC endpoint of the instance currently holding
// the IPC primary role, or ok=false when it cannot tell (no lock info, lock
// just released). The runtime wires one that reads the IPC lock file.
type PrimaryResolver func() (rpcAddr string, ok bool)

// DefaultHandoverWait bounds how long a forwarded write keeps waiting for a
// primary that is unreachable or handing its role over (drain → lock release
// → instance.shutdown → a secondary promotes and rebinds the ports). A
// graceful handover completes in well under a second; the bound also covers
// most of the 15 s heartbeat gap after a primary is SIGKILLed. One call
// already in flight when the bound expires may add up to its own timeout.
const DefaultHandoverWait = 20 * time.Second

// Backoff between forwarding retries: doubles from min to max, jittered.
const (
	forwardRetryBackoffMin = 50 * time.Millisecond
	forwardRetryBackoffMax = time.Second
	// forwardMaxAttemptsPerClass caps retries for BUSY answers and for
	// ambiguous outcomes (timeout / lost response), exactly like the legacy
	// 3-attempt loop. Unreachable/unavailable primaries are bounded by the
	// handover wait instead.
	forwardMaxAttemptsPerClass = 3
)

// ErrNotRemote is returned by the forwarding helpers (WriteWithRetry,
// ProxyWriteWithResult) when the proxy has no IPC client, i.e. this instance is
// the primary's local writer. A caller that checked IsRemote() just before the
// call can still see it when a failover promotion raced the call; it should
// then perform the write directly, as a primary would.
var ErrNotRemote = errors.New("dbproxy: not forwarding writes: this instance is the local writer")

// New creates a DBProxy backed by local for reads and direct write attempts.
// Pass a non-nil client and the primary's rpcAddr to enable write proxying.
// Pass client=nil for primary instances (writes go directly to the local querier).
func New(local db.Querier, client *ipc.Client, rpcAddr string) *DBProxy {
	return NewWithInstanceID(local, client, rpcAddr, "")
}

// NewWithInstanceID is like New but records the caller's instance ID in every
// WriteMeta so the primary can attribute writes to the originating secondary.
func NewWithInstanceID(local db.Querier, client *ipc.Client, rpcAddr, instanceID string) *DBProxy {
	p := &DBProxy{
		Querier:    local,
		instanceID: instanceID,
	}
	p.rpcAddr.Store(&rpcAddr)
	if client != nil {
		p.client.Store(client)
	}
	return p
}

// RPCAddr returns the primary endpoint writes are currently forwarded to.
func (p *DBProxy) RPCAddr() string {
	if p == nil {
		return ""
	}
	if a := p.rpcAddr.Load(); a != nil {
		return *a
	}
	return ""
}

// SetPrimaryResolver installs the function consulted between forwarding
// retries to re-point the proxy at the current primary after a failover.
// Pass nil to remove it.
func (p *DBProxy) SetPrimaryResolver(r PrimaryResolver) {
	if r == nil {
		p.resolver.Store(nil)
		return
	}
	p.resolver.Store(&r)
}

// SetHandoverWait overrides DefaultHandoverWait for this proxy (tests, or a
// caller that must fail faster). d <= 0 restores the default.
func (p *DBProxy) SetHandoverWait(d time.Duration) {
	if d < 0 {
		d = 0
	}
	p.handoverWait.Store(int64(d))
}

func (p *DBProxy) handoverWaitBound() time.Duration {
	if d := time.Duration(p.handoverWait.Load()); d > 0 {
		return d
	}
	return DefaultHandoverWait
}

// refreshPrimaryAddr re-points the proxy at the endpoint the resolver
// reports, when it reports one that differs from the current one.
func (p *DBProxy) refreshPrimaryAddr() {
	rp := p.resolver.Load()
	if rp == nil || *rp == nil {
		return
	}
	addr, ok := (*rp)()
	if !ok || addr == "" {
		return
	}
	if old := p.rpcAddr.Swap(&addr); old == nil || *old != addr {
		prev := ""
		if old != nil {
			prev = *old
		}
		logging.Info("dbproxy: primary endpoint changed, re-pointing forwarded writes",
			"old", prev, "new", addr)
	}
}

// IsRemote reports whether writes may be forwarded to another (primary)
// instance over IPC. It is false for a proxy built without a client and after
// Promote. Safe to call on a nil *DBProxy (reports false), so stores can test
// an optional proxy with a single call.
func (p *DBProxy) IsRemote() bool {
	return p != nil && p.client.Load() != nil
}

// Promote atomically turns the proxy into a pure passthrough to its local
// querier: from now on no write is forwarded over IPC. It is called when this
// instance wins a failover promotion and its local pool has become the
// primary's writer. It returns the IPC client the proxy used (nil if it had
// none) so the caller decides when to close it; writes already in flight keep
// the client snapshot they read and finish (or fail) against it.
func (p *DBProxy) Promote() *ipc.Client {
	return p.client.Swap(nil)
}

// isPrimary returns true when no write proxying is configured.
func (p *DBProxy) isPrimary() bool { return !p.IsRemote() }

// ProbePrimary sends an instance.ping JSON-RPC call to the primary with a 2-second
// timeout and returns nil on success.  Returns an error if the primary is
// unreachable, the call times out, or the proxy is not configured.
// Always returns nil when this instance is the primary (no proxy needed).
func (p *DBProxy) ProbePrimary(ctx context.Context) error {
	client := p.client.Load()
	if client == nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := client.Call(probeCtx, p.RPCAddr(), "instance.ping", struct{}{})
	if err != nil {
		return fmt.Errorf("dbproxy: probe primary: %w", err)
	}
	return nil
}

// newMeta builds a WriteMeta for the current request.
func (p *DBProxy) newMeta() WriteMeta {
	return WriteMeta{
		SourceInstanceID: p.instanceID,
		RequestID:        fmt.Sprintf("req-%d", time.Now().UnixNano()),
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
	}
}

// isLockError returns true when err indicates that SQLite could not acquire the
// write lock (SQLITE_BUSY or SQLITE_LOCKED). These are the only errors that
// trigger the IPC proxy fallback; all other errors are returned as-is.
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	// The ncruces driver does not always wrap failures in *sqlite3.Error: a
	// plain BeginTx lock collision (verified empirically — see
	// TestMapToWriteError_BusyAndLockedErrorsAreErrCodeBusy) surfaces as a
	// bare sqlite3.ExtendedErrorCode/sqlite3.ErrorCode value instead, which
	// errors.As into *sqlite3.Error does not match. errors.Is covers both
	// shapes: *sqlite3.Error.Is compares codes directly, and
	// ExtendedErrorCode.Is compares against an ErrorCode target the same way.
	if errors.Is(err, sqlite3.BUSY) || errors.Is(err, sqlite3.LOCKED) {
		return true
	}
	var e *sqlite3.Error
	if errors.As(err, &e) {
		c := e.Code()
		return c == sqlite3.BUSY || c == sqlite3.LOCKED
	}
	// String fallback for wrapped or driver-specific error representations,
	// and for an error that already crossed the IPC boundary as plain text
	// (the primary's remembrances dispatcher returns the raw store error,
	// which the JSON-RPC layer flattens to a message-only rpcError before the
	// secondary's ipc.Client.Call re-wraps it — no typed error survives that
	// round trip either way).
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "database is locked") ||
		strings.Contains(s, "database table is locked") ||
		strings.Contains(s, "sqlite_busy") ||
		strings.Contains(s, "sqlite_locked")
}

// ---- proxy helpers ----

// proxyWrite serialises params, sends db.write to the primary within the given
// timeout, and deserialises the result.
func proxyWrite[R any](ctx context.Context, p *DBProxy, method string, params any, timeout time.Duration) (R, error) {
	var zero R
	client := p.client.Load()
	if client == nil {
		return zero, ErrNotRemote
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return zero, fmt.Errorf("dbproxy: marshal params for %s: %w", method, err)
	}
	req := WriteRequest{Meta: p.newMeta(), Method: method, Params: rawParams}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := client.Call(callCtx, p.RPCAddr(), MethodDBWrite, req)
	if err != nil {
		return zero, mapToWriteError(method, err)
	}
	var result R
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("dbproxy: unmarshal result for %s: %w", method, err)
	}
	return result, nil
}

// ProxyWriteWithResult forwards a write method and decodes a typed result.
// It waits out a primary handover (see forwardWithHandoverRetry) but never
// re-sends a write whose outcome is unknown: typed writes are mostly creates,
// and a re-sent create that had in fact been applied would fail with a
// conflict after succeeding.
func ProxyWriteWithResult[R any](ctx context.Context, p *DBProxy, method string, params any) (R, error) {
	return proxyWriteRetry[R](ctx, p, method, params, DefaultWriteTimeouts.Default)
}

// proxyWriteRetry is proxyWrite wrapped in the handover-tolerant retry loop,
// without re-sending ambiguous outcomes.
func proxyWriteRetry[R any](ctx context.Context, p *DBProxy, method string, params any, timeout time.Duration) (R, error) {
	var result R
	err := p.forwardWithHandoverRetry(ctx, method, false, func(ctx context.Context) error {
		r, err := proxyWrite[R](ctx, p, method, params, timeout)
		if err == nil {
			result = r
		}
		return err
	})
	return result, err
}

// proxyVoidWrite sends a write that returns only an error.
func proxyVoidWrite(ctx context.Context, p *DBProxy, method string, params any, timeout time.Duration) error {
	client := p.client.Load()
	if client == nil {
		return ErrNotRemote
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("dbproxy: marshal params for %s: %w", method, err)
	}
	req := WriteRequest{Meta: p.newMeta(), Method: method, Params: rawParams}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err = client.Call(callCtx, p.RPCAddr(), MethodDBWrite, req)
	if err != nil {
		return mapToWriteError(method, err)
	}
	return nil
}

// WriteWithRetry forwards a void write with the provided timeout and retry logic.
// It returns an error wrapping ErrNotRemote when the proxy has no client (see
// ErrNotRemote for how callers should react).
func (p *DBProxy) WriteWithRetry(ctx context.Context, method string, params any, timeout time.Duration) error {
	if p.isPrimary() {
		return fmt.Errorf("dbproxy: WriteWithRetry %s: %w", method, ErrNotRemote)
	}
	return p.writeWithRetry(ctx, method, params, timeout)
}

// writeWithRetry forwards a void write through the handover-tolerant retry
// loop. Void writes keep the legacy behaviour of re-sending an ambiguous
// outcome (timeout) up to forwardMaxAttemptsPerClass times: they are mostly
// idempotent replaces/deletes/updates.
func (p *DBProxy) writeWithRetry(ctx context.Context, method string, params any, timeout time.Duration) error {
	return p.forwardWithHandoverRetry(ctx, method, true, func(ctx context.Context) error {
		return proxyVoidWrite(ctx, p, method, params, timeout)
	})
}

// forwardWithHandoverRetry runs attempt (one forwarded call) until it
// succeeds or fails for good. Per error class:
//
//   - ErrNotRemote: this instance was promoted meanwhile; returned at once so
//     the caller performs the write locally (every caller already does).
//   - Unreachable (the request was never delivered) and Unavailable (the
//     primary refused it while draining for a handover): nothing was applied,
//     so the write is re-sent, re-resolving the primary endpoint before each
//     attempt, until the handover wait (DefaultHandoverWait, 20 s) expires.
//   - Busy (the primary's own busy timeout expired): re-sent, at most
//     forwardMaxAttemptsPerClass attempts in total, as before.
//   - Timeout / lost response (outcome unknown): re-sent only when
//     retryAmbiguous, at most forwardMaxAttemptsPerClass attempts.
//   - anything else (constraint violation, invalid params, unknown method,
//     SQL error): returned at once.
//
// Waits are jittered exponential backoff (50 ms → 1 s) and honour ctx. When
// the handover wait is exhausted the last error is returned wrapped in a
// message that says so (errors.As still finds the *WriteError).
func (p *DBProxy) forwardWithHandoverRetry(ctx context.Context, method string, retryAmbiguous bool, attempt func(context.Context) error) error {
	started := time.Now()
	bound := p.handoverWaitBound()
	deadline := started.Add(bound)
	backoff := forwardRetryBackoffMin
	busyAttempts, ambiguousAttempts := 0, 0
	waitingForPrimary := false

	for n := 1; ; n++ {
		err := attempt(ctx)
		if err == nil {
			if waitingForPrimary {
				logging.Info("dbproxy: forwarded write accepted after waiting for the primary",
					"method", method, "attempts", n, "waited", time.Since(started).Round(time.Millisecond),
					"primary_rpc", p.RPCAddr())
			}
			return nil
		}
		if errors.Is(err, ErrNotRemote) {
			return err
		}
		var werr *WriteError
		if !errors.As(err, &werr) {
			return err
		}
		switch werr.Code {
		case ErrCodeUnreachable, ErrCodeUnavailable:
			if werr.Code == ErrCodeUnavailable {
				// The primary answered that it is going away: its ROUTER is
				// about to close, so re-dial for the next attempt instead of
				// writing into a socket whose peer is gone.
				if c := p.client.Load(); c != nil {
					c.ForgetEndpoint(p.RPCAddr())
				}
			}
			if !waitingForPrimary {
				logging.Info("dbproxy: primary unreachable or handing over, waiting for a primary",
					"method", method, "code", werr.Code, "max_wait", bound, "error", werr.Message)
			}
			waitingForPrimary = true
		case ErrCodeBusy:
			busyAttempts++
			if busyAttempts >= forwardMaxAttemptsPerClass {
				return err
			}
		case ErrCodeTimeout:
			ambiguousAttempts++
			if !retryAmbiguous || ambiguousAttempts >= forwardMaxAttemptsPerClass {
				return err
			}
		default:
			return err
		}

		delay := jitterDuration(backoff)
		if time.Until(deadline) < delay {
			if waitingForPrimary {
				return fmt.Errorf("dbproxy: %s: no primary accepted the write within %s (waited for a handover, %d attempts): %w",
					method, bound, n, err)
			}
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("dbproxy: %s: gave up waiting for the primary: %v: %w", method, ctx.Err(), err)
		case <-timer.C:
		}
		backoff = min(backoff*2, forwardRetryBackoffMax)
		p.refreshPrimaryAddr()
	}
}

// jitterDuration returns d scaled by a random factor in [0.75, 1.25).
func jitterDuration(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.75 + rand.Float64()*0.5))
}

// ---- direct-then-proxy helpers ----

// directOrProxy attempts a typed write directly against the local RW Querier.
// On success the result is returned immediately. On SQLITE_BUSY/LOCKED the
// write is forwarded to the primary via ZMQ and the proxy result is returned.
// Any other error is returned as-is without proxying.
func directOrProxy[R any](
	ctx context.Context,
	p *DBProxy,
	directFn func() (R, error),
	method string,
	params any,
	timeout time.Duration,
) (R, error) {
	if p.isPrimary() {
		return directFn()
	}
	result, err := directFn()
	if err == nil {
		return result, nil
	}
	if !isLockError(err) {
		return result, err
	}
	logging.Debug("dbproxy: direct write got lock contention, falling back to proxy",
		"method", method, "error", err)
	proxied, perr := proxyWriteRetry[R](ctx, p, method, params, timeout)
	if errors.Is(perr, ErrNotRemote) {
		// Promoted between the direct attempt and the fallback: this pool is
		// now the primary's writer (with its long busy_timeout), so retry
		// directly instead of forwarding.
		return directFn()
	}
	return proxied, perr
}

// directOrProxyVoid is like directOrProxy but for operations that return only
// an error (no result value).
func directOrProxyVoid(
	ctx context.Context,
	p *DBProxy,
	directFn func() error,
	method string,
	params any,
	timeout time.Duration,
) error {
	if p.isPrimary() {
		return directFn()
	}
	if err := directFn(); err == nil {
		return nil
	} else if !isLockError(err) {
		return err
	}
	logging.Debug("dbproxy: direct void write got lock contention, falling back to proxy",
		"method", method)
	err := p.writeWithRetry(ctx, method, params, timeout)
	if errors.Is(err, ErrNotRemote) {
		// Promoted between the direct attempt and the fallback (see directOrProxy).
		return directFn()
	}
	return err
}

// Forward sends a void write to the primary when this proxy is remote. It
// reports forwarded=false — and the caller must perform the write directly
// against its local connection — when p is nil, has no client, or lost it to a
// failover promotion that raced this call (ErrNotRemote). When forwarded is
// true, err is the outcome of the forwarded write.
//
// This is the single check stores use for "write through the primary or
// locally", instead of testing p != nil: after Promote a store's proxy is
// still non-nil but must no longer forward.
func (p *DBProxy) Forward(ctx context.Context, method string, params any, timeout time.Duration) (forwarded bool, err error) {
	if !p.IsRemote() {
		return false, nil
	}
	err = p.WriteWithRetry(ctx, method, params, timeout)
	if errors.Is(err, ErrNotRemote) {
		return false, nil
	}
	return true, err
}

// ForwardWithResult is Forward for a write that returns a typed result.
func ForwardWithResult[R any](ctx context.Context, p *DBProxy, method string, params any) (result R, forwarded bool, err error) {
	if !p.IsRemote() {
		return result, false, nil
	}
	result, err = ProxyWriteWithResult[R](ctx, p, method, params)
	if errors.Is(err, ErrNotRemote) {
		return result, false, nil
	}
	return result, true, err
}

// ---- Write method overrides ----

func (p *DBProxy) CreateSession(ctx context.Context, arg db.CreateSessionParams) (db.Session, error) {
	return directOrProxy(ctx, p,
		func() (db.Session, error) { return p.Querier.CreateSession(ctx, arg) },
		"CreateSession", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateSession(ctx context.Context, arg db.UpdateSessionParams) (db.Session, error) {
	return directOrProxy(ctx, p,
		func() (db.Session, error) { return p.Querier.UpdateSession(ctx, arg) },
		"UpdateSession", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteSession(ctx context.Context, id string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeleteSession(ctx, id) },
		"DeleteSession", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeleteSessionMessages(ctx, sessionID) },
		"DeleteSessionMessages", sessionID, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) CreateMessage(ctx context.Context, arg db.CreateMessageParams) (db.Message, error) {
	return directOrProxy(ctx, p,
		func() (db.Message, error) { return p.Querier.CreateMessage(ctx, arg) },
		"CreateMessage", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateMessage(ctx context.Context, arg db.UpdateMessageParams) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.UpdateMessage(ctx, arg) },
		"UpdateMessage", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteMessage(ctx context.Context, id string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeleteMessage(ctx, id) },
		"DeleteMessage", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) CreateFile(ctx context.Context, arg db.CreateFileParams) (db.File, error) {
	return directOrProxy(ctx, p,
		func() (db.File, error) { return p.Querier.CreateFile(ctx, arg) },
		"CreateFile", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateFile(ctx context.Context, arg db.UpdateFileParams) (db.File, error) {
	return directOrProxy(ctx, p,
		func() (db.File, error) { return p.Querier.UpdateFile(ctx, arg) },
		"UpdateFile", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteFile(ctx context.Context, id string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeleteFile(ctx, id) },
		"DeleteFile", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteSessionFiles(ctx context.Context, sessionID string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeleteSessionFiles(ctx, sessionID) },
		"DeleteSessionFiles", sessionID, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) InsertPromptTemplate(ctx context.Context, arg db.InsertPromptTemplateParams) (db.PromptTemplate, error) {
	return directOrProxy(ctx, p,
		func() (db.PromptTemplate, error) { return p.Querier.InsertPromptTemplate(ctx, arg) },
		"InsertPromptTemplate", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) InsertSessionScore(ctx context.Context, arg db.InsertSessionScoreParams) (db.SessionScore, error) {
	return directOrProxy(ctx, p,
		func() (db.SessionScore, error) { return p.Querier.InsertSessionScore(ctx, arg) },
		"InsertSessionScore", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) InsertSkill(ctx context.Context, arg db.InsertSkillParams) (db.SkillLibrary, error) {
	return directOrProxy(ctx, p,
		func() (db.SkillLibrary, error) { return p.Querier.InsertSkill(ctx, arg) },
		"InsertSkill", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeactivateLowestSkill(ctx context.Context) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeactivateLowestSkill(ctx) },
		"DeactivateLowestSkill", nil, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) IncrementSkillUsage(ctx context.Context, id string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.IncrementSkillUsage(ctx, id) },
		"IncrementSkillUsage", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error) {
	return directOrProxy(ctx, p,
		func() (db.Project, error) { return p.Querier.CreateProject(ctx, arg) },
		"CreateProject", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateProjectStatus(ctx context.Context, arg db.UpdateProjectStatusParams) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.UpdateProjectStatus(ctx, arg) },
		"UpdateProjectStatus", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateProjectLastOpened(ctx context.Context, arg db.UpdateProjectLastOpenedParams) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.UpdateProjectLastOpened(ctx, arg) },
		"UpdateProjectLastOpened", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) MarkProjectInitialized(ctx context.Context, id string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.MarkProjectInitialized(ctx, id) },
		"MarkProjectInitialized", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteProject(ctx context.Context, id string) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.DeleteProject(ctx, id) },
		"DeleteProject", id, DefaultWriteTimeouts.Default)
}

// ProjectNameUpdater is the project rename write, which is hand-written on
// *db.Queries and therefore not part of the generated db.Querier interface.
type ProjectNameUpdater interface {
	UpdateProjectName(ctx context.Context, id, name string) error
}

// UpdateProjectNameParams is the forwarded payload of UpdateProjectName.
type UpdateProjectNameParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UpdateProjectName renames a project: direct first, forwarded on lock
// contention like every other sqlc write. The local querier must implement
// ProjectNameUpdater (*db.Queries does).
func (p *DBProxy) UpdateProjectName(ctx context.Context, id, name string) error {
	u, ok := p.Querier.(ProjectNameUpdater)
	if !ok {
		return fmt.Errorf("dbproxy: UpdateProjectName: local querier %T cannot rename projects", p.Querier)
	}
	return directOrProxyVoid(ctx, p,
		func() error { return u.UpdateProjectName(ctx, id, name) },
		"UpdateProjectName", UpdateProjectNameParams{ID: id, Name: name}, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateSessionACPState(ctx context.Context, arg db.UpdateSessionACPStateParams) error {
	return directOrProxyVoid(ctx, p,
		func() error { return p.Querier.UpdateSessionACPState(ctx, arg) },
		"UpdateSessionACPState", arg, DefaultWriteTimeouts.Default)
}

// Ensure DBProxy satisfies db.Querier at compile time.
var _ db.Querier = (*DBProxy)(nil)
