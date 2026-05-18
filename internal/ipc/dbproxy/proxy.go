// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package dbproxy provides a db.Querier implementation that transparently
// routes write operations to the primary Pando instance via ZMQ JSON-RPC,
// while serving reads from the local (possibly read-only) SQLite database.
//
// Secondary instances use DBProxy so they never write to SQLite directly,
// preserving the single-writer invariant required by SQLite.
package dbproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
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
// querier. Writes are forwarded via ZMQ JSON-RPC to the primary instance.
//
// When client is nil the proxy behaves identically to the embedded querier
// (useful for the primary instance itself).
type DBProxy struct {
	db.Querier             // local reads — embedded interface
	client     *ipc.Client
	rpcAddr    string
	instanceID string
}

// New creates a DBProxy backed by local for reads.
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

// newMeta builds a WriteMeta for the current request.
func (p *DBProxy) newMeta() WriteMeta {
	return WriteMeta{
		SourceInstanceID: p.instanceID,
		RequestID:        fmt.Sprintf("req-%d", time.Now().UnixNano()),
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
	}
}

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

// ---- Write method overrides ----

func (p *DBProxy) CreateSession(ctx context.Context, arg db.CreateSessionParams) (db.Session, error) {
	if p.isPrimary() {
		return p.Querier.CreateSession(ctx, arg)
	}
	return proxyWrite[db.Session](ctx, p, "CreateSession", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateSession(ctx context.Context, arg db.UpdateSessionParams) (db.Session, error) {
	if p.isPrimary() {
		return p.Querier.UpdateSession(ctx, arg)
	}
	return proxyWrite[db.Session](ctx, p, "UpdateSession", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteSession(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteSession(ctx, id)
	}
	return p.writeWithRetry(ctx, "DeleteSession", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	if p.isPrimary() {
		return p.Querier.DeleteSessionMessages(ctx, sessionID)
	}
	return p.writeWithRetry(ctx, "DeleteSessionMessages", sessionID, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) CreateMessage(ctx context.Context, arg db.CreateMessageParams) (db.Message, error) {
	if p.isPrimary() {
		return p.Querier.CreateMessage(ctx, arg)
	}
	return proxyWrite[db.Message](ctx, p, "CreateMessage", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateMessage(ctx context.Context, arg db.UpdateMessageParams) error {
	if p.isPrimary() {
		return p.Querier.UpdateMessage(ctx, arg)
	}
	return p.writeWithRetry(ctx, "UpdateMessage", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteMessage(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteMessage(ctx, id)
	}
	return p.writeWithRetry(ctx, "DeleteMessage", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) CreateFile(ctx context.Context, arg db.CreateFileParams) (db.File, error) {
	if p.isPrimary() {
		return p.Querier.CreateFile(ctx, arg)
	}
	return proxyWrite[db.File](ctx, p, "CreateFile", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateFile(ctx context.Context, arg db.UpdateFileParams) (db.File, error) {
	if p.isPrimary() {
		return p.Querier.UpdateFile(ctx, arg)
	}
	return proxyWrite[db.File](ctx, p, "UpdateFile", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteFile(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteFile(ctx, id)
	}
	return p.writeWithRetry(ctx, "DeleteFile", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteSessionFiles(ctx context.Context, sessionID string) error {
	if p.isPrimary() {
		return p.Querier.DeleteSessionFiles(ctx, sessionID)
	}
	return p.writeWithRetry(ctx, "DeleteSessionFiles", sessionID, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) InsertPromptTemplate(ctx context.Context, arg db.InsertPromptTemplateParams) (db.PromptTemplate, error) {
	if p.isPrimary() {
		return p.Querier.InsertPromptTemplate(ctx, arg)
	}
	return proxyWrite[db.PromptTemplate](ctx, p, "InsertPromptTemplate", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) InsertSessionScore(ctx context.Context, arg db.InsertSessionScoreParams) (db.SessionScore, error) {
	if p.isPrimary() {
		return p.Querier.InsertSessionScore(ctx, arg)
	}
	return proxyWrite[db.SessionScore](ctx, p, "InsertSessionScore", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) InsertSkill(ctx context.Context, arg db.InsertSkillParams) (db.SkillLibrary, error) {
	if p.isPrimary() {
		return p.Querier.InsertSkill(ctx, arg)
	}
	return proxyWrite[db.SkillLibrary](ctx, p, "InsertSkill", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeactivateLowestSkill(ctx context.Context) error {
	if p.isPrimary() {
		return p.Querier.DeactivateLowestSkill(ctx)
	}
	return p.writeWithRetry(ctx, "DeactivateLowestSkill", nil, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) IncrementSkillUsage(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.IncrementSkillUsage(ctx, id)
	}
	return p.writeWithRetry(ctx, "IncrementSkillUsage", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error) {
	if p.isPrimary() {
		return p.Querier.CreateProject(ctx, arg)
	}
	return proxyWrite[db.Project](ctx, p, "CreateProject", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateProjectStatus(ctx context.Context, arg db.UpdateProjectStatusParams) error {
	if p.isPrimary() {
		return p.Querier.UpdateProjectStatus(ctx, arg)
	}
	return p.writeWithRetry(ctx, "UpdateProjectStatus", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) UpdateProjectLastOpened(ctx context.Context, arg db.UpdateProjectLastOpenedParams) error {
	if p.isPrimary() {
		return p.Querier.UpdateProjectLastOpened(ctx, arg)
	}
	return p.writeWithRetry(ctx, "UpdateProjectLastOpened", arg, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) MarkProjectInitialized(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.MarkProjectInitialized(ctx, id)
	}
	return p.writeWithRetry(ctx, "MarkProjectInitialized", id, DefaultWriteTimeouts.Default)
}

func (p *DBProxy) DeleteProject(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteProject(ctx, id)
	}
	return p.writeWithRetry(ctx, "DeleteProject", id, DefaultWriteTimeouts.Default)
}

// Ensure DBProxy satisfies db.Querier at compile time.
var _ db.Querier = (*DBProxy)(nil)
