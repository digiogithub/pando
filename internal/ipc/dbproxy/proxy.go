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
	"strings"
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
type DBProxy struct {
	db.Querier // local reads and direct write attempts — embedded interface
	client     *ipc.Client
	rpcAddr    string
	instanceID string
}

// New creates a DBProxy backed by local for reads and direct write attempts.
// Pass a non-nil client and the primary's rpcAddr to enable write proxying.
// Pass client=nil for primary instances (writes go directly to the local querier).
func New(local db.Querier, client *ipc.Client, rpcAddr string) *DBProxy {
	return &DBProxy{
		Querier: local,
		client:  client,
		rpcAddr: rpcAddr,
	}
}

// NewWithInstanceID is like New but records the caller's instance ID in every
// WriteMeta so the primary can attribute writes to the originating secondary.
func NewWithInstanceID(local db.Querier, client *ipc.Client, rpcAddr, instanceID string) *DBProxy {
	return &DBProxy{
		Querier:    local,
		client:     client,
		rpcAddr:    rpcAddr,
		instanceID: instanceID,
	}
}

// isPrimary returns true when no write proxying is configured.
func (p *DBProxy) isPrimary() bool { return p.client == nil }

// ProbePrimary sends an instance.ping JSON-RPC call to the primary with a 2-second
// timeout and returns nil on success.  Returns an error if the primary is
// unreachable, the call times out, or the proxy is not configured.
// Always returns nil when this instance is the primary (no proxy needed).
func (p *DBProxy) ProbePrimary(ctx context.Context) error {
	if p.isPrimary() {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := p.client.Call(probeCtx, p.rpcAddr, "instance.ping", struct{}{})
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
	var e *sqlite3.Error
	if errors.As(err, &e) {
		c := e.Code()
		return c == sqlite3.BUSY || c == sqlite3.LOCKED
	}
	// String fallback for wrapped or driver-specific error representations.
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
	rawParams, err := json.Marshal(params)
	if err != nil {
		return zero, fmt.Errorf("dbproxy: marshal params for %s: %w", method, err)
	}
	req := WriteRequest{Meta: p.newMeta(), Method: method, Params: rawParams}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := p.client.Call(callCtx, p.rpcAddr, MethodDBWrite, req)
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
func ProxyWriteWithResult[R any](ctx context.Context, p *DBProxy, method string, params any) (R, error) {
	return proxyWrite[R](ctx, p, method, params, DefaultWriteTimeouts.Default)
}

// proxyVoidWrite sends a write that returns only an error.
func proxyVoidWrite(ctx context.Context, p *DBProxy, method string, params any, timeout time.Duration) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("dbproxy: marshal params for %s: %w", method, err)
	}
	req := WriteRequest{Meta: p.newMeta(), Method: method, Params: rawParams}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err = p.client.Call(callCtx, p.rpcAddr, MethodDBWrite, req)
	if err != nil {
		return mapToWriteError(method, err)
	}
	return nil
}

// WriteWithRetry forwards a void write with the provided timeout and retry logic.
func (p *DBProxy) WriteWithRetry(ctx context.Context, method string, params any, timeout time.Duration) error {
	if p.isPrimary() {
		return fmt.Errorf("dbproxy: WriteWithRetry requires a configured client")
	}
	return p.writeWithRetry(ctx, method, params, timeout)
}

// writeWithRetry retries proxyVoidWrite on transient errors with exponential backoff.
// Maximum 3 attempts starting with a 50ms delay.
func (p *DBProxy) writeWithRetry(ctx context.Context, method string, params any, timeout time.Duration) error {
	const maxRetries = 3
	backoff := 50 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := proxyVoidWrite(ctx, p, method, params, timeout)
		if err == nil {
			return nil
		}

		var werr *WriteError
		if errors.As(err, &werr) && werr.IsRetryable() && attempt < maxRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		return err
	}
	return fmt.Errorf("dbproxy: exhausted retries for %s", method)
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
	return proxyWrite[R](ctx, p, method, params, timeout)
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
	return p.writeWithRetry(ctx, method, params, timeout)
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

// Ensure DBProxy satisfies db.Querier at compile time.
var _ db.Querier = (*DBProxy)(nil)
