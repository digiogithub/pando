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
	"fmt"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
)

// MethodDBWrite is the JSON-RPC method name for proxied write operations.
const MethodDBWrite = "db.write"

// WriteRequest is the JSON-RPC params struct for a proxied write.
type WriteRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// DBProxy implements db.Querier. Reads are served from the embedded local
// querier. Writes are forwarded via ZMQ JSON-RPC to the primary instance.
//
// When client is nil the proxy behaves identically to the embedded querier
// (useful for the primary instance itself).
type DBProxy struct {
	db.Querier        // local reads — embedded interface
	client    *ipc.Client
	rpcAddr   string
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

// isPrimary returns true when no write proxying is configured.
func (p *DBProxy) isPrimary() bool { return p.client == nil }

// proxyWrite serialises params, sends db.write to the primary, and deserialises the result.
func proxyWrite[R any](ctx context.Context, p *DBProxy, method string, params any) (R, error) {
	var zero R
	rawParams, err := json.Marshal(params)
	if err != nil {
		return zero, fmt.Errorf("dbproxy: marshal params for %s: %w", method, err)
	}
	req := WriteRequest{Method: method, Params: rawParams}
	raw, err := p.client.Call(ctx, p.rpcAddr, MethodDBWrite, req)
	if err != nil {
		return zero, fmt.Errorf("dbproxy: remote %s: %w", method, err)
	}
	var result R
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("dbproxy: unmarshal result for %s: %w", method, err)
	}
	return result, nil
}

// proxyVoidWrite sends a write that returns only an error.
func proxyVoidWrite(ctx context.Context, p *DBProxy, method string, params any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("dbproxy: marshal params for %s: %w", method, err)
	}
	req := WriteRequest{Method: method, Params: rawParams}
	_, err = p.client.Call(ctx, p.rpcAddr, MethodDBWrite, req)
	if err != nil {
		return fmt.Errorf("dbproxy: remote %s: %w", method, err)
	}
	return nil
}

// ---- Write method overrides ----

func (p *DBProxy) CreateSession(ctx context.Context, arg db.CreateSessionParams) (db.Session, error) {
	if p.isPrimary() {
		return p.Querier.CreateSession(ctx, arg)
	}
	return proxyWrite[db.Session](ctx, p, "CreateSession", arg)
}

func (p *DBProxy) UpdateSession(ctx context.Context, arg db.UpdateSessionParams) (db.Session, error) {
	if p.isPrimary() {
		return p.Querier.UpdateSession(ctx, arg)
	}
	return proxyWrite[db.Session](ctx, p, "UpdateSession", arg)
}

func (p *DBProxy) DeleteSession(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteSession(ctx, id)
	}
	return proxyVoidWrite(ctx, p, "DeleteSession", id)
}

func (p *DBProxy) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	if p.isPrimary() {
		return p.Querier.DeleteSessionMessages(ctx, sessionID)
	}
	return proxyVoidWrite(ctx, p, "DeleteSessionMessages", sessionID)
}

func (p *DBProxy) CreateMessage(ctx context.Context, arg db.CreateMessageParams) (db.Message, error) {
	if p.isPrimary() {
		return p.Querier.CreateMessage(ctx, arg)
	}
	return proxyWrite[db.Message](ctx, p, "CreateMessage", arg)
}

func (p *DBProxy) UpdateMessage(ctx context.Context, arg db.UpdateMessageParams) error {
	if p.isPrimary() {
		return p.Querier.UpdateMessage(ctx, arg)
	}
	return proxyVoidWrite(ctx, p, "UpdateMessage", arg)
}

func (p *DBProxy) DeleteMessage(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteMessage(ctx, id)
	}
	return proxyVoidWrite(ctx, p, "DeleteMessage", id)
}

func (p *DBProxy) CreateFile(ctx context.Context, arg db.CreateFileParams) (db.File, error) {
	if p.isPrimary() {
		return p.Querier.CreateFile(ctx, arg)
	}
	return proxyWrite[db.File](ctx, p, "CreateFile", arg)
}

func (p *DBProxy) UpdateFile(ctx context.Context, arg db.UpdateFileParams) (db.File, error) {
	if p.isPrimary() {
		return p.Querier.UpdateFile(ctx, arg)
	}
	return proxyWrite[db.File](ctx, p, "UpdateFile", arg)
}

func (p *DBProxy) DeleteFile(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteFile(ctx, id)
	}
	return proxyVoidWrite(ctx, p, "DeleteFile", id)
}

func (p *DBProxy) DeleteSessionFiles(ctx context.Context, sessionID string) error {
	if p.isPrimary() {
		return p.Querier.DeleteSessionFiles(ctx, sessionID)
	}
	return proxyVoidWrite(ctx, p, "DeleteSessionFiles", sessionID)
}

func (p *DBProxy) InsertPromptTemplate(ctx context.Context, arg db.InsertPromptTemplateParams) (db.PromptTemplate, error) {
	if p.isPrimary() {
		return p.Querier.InsertPromptTemplate(ctx, arg)
	}
	return proxyWrite[db.PromptTemplate](ctx, p, "InsertPromptTemplate", arg)
}

func (p *DBProxy) InsertSessionScore(ctx context.Context, arg db.InsertSessionScoreParams) (db.SessionScore, error) {
	if p.isPrimary() {
		return p.Querier.InsertSessionScore(ctx, arg)
	}
	return proxyWrite[db.SessionScore](ctx, p, "InsertSessionScore", arg)
}

func (p *DBProxy) InsertSkill(ctx context.Context, arg db.InsertSkillParams) (db.SkillLibrary, error) {
	if p.isPrimary() {
		return p.Querier.InsertSkill(ctx, arg)
	}
	return proxyWrite[db.SkillLibrary](ctx, p, "InsertSkill", arg)
}

func (p *DBProxy) DeactivateLowestSkill(ctx context.Context) error {
	if p.isPrimary() {
		return p.Querier.DeactivateLowestSkill(ctx)
	}
	return proxyVoidWrite(ctx, p, "DeactivateLowestSkill", nil)
}

func (p *DBProxy) IncrementSkillUsage(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.IncrementSkillUsage(ctx, id)
	}
	return proxyVoidWrite(ctx, p, "IncrementSkillUsage", id)
}

func (p *DBProxy) CreateProject(ctx context.Context, arg db.CreateProjectParams) (db.Project, error) {
	if p.isPrimary() {
		return p.Querier.CreateProject(ctx, arg)
	}
	return proxyWrite[db.Project](ctx, p, "CreateProject", arg)
}

func (p *DBProxy) UpdateProjectStatus(ctx context.Context, arg db.UpdateProjectStatusParams) error {
	if p.isPrimary() {
		return p.Querier.UpdateProjectStatus(ctx, arg)
	}
	return proxyVoidWrite(ctx, p, "UpdateProjectStatus", arg)
}

func (p *DBProxy) UpdateProjectLastOpened(ctx context.Context, arg db.UpdateProjectLastOpenedParams) error {
	if p.isPrimary() {
		return p.Querier.UpdateProjectLastOpened(ctx, arg)
	}
	return proxyVoidWrite(ctx, p, "UpdateProjectLastOpened", arg)
}

func (p *DBProxy) MarkProjectInitialized(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.MarkProjectInitialized(ctx, id)
	}
	return proxyVoidWrite(ctx, p, "MarkProjectInitialized", id)
}

func (p *DBProxy) DeleteProject(ctx context.Context, id string) error {
	if p.isPrimary() {
		return p.Querier.DeleteProject(ctx, id)
	}
	return proxyVoidWrite(ctx, p, "DeleteProject", id)
}

// Ensure DBProxy satisfies db.Querier at compile time.
var _ db.Querier = (*DBProxy)(nil)
