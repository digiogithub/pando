// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package remoteview provides types for observing and controlling remote Pando
// instances over the IPC ZeroMQ bus.
package remoteview

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	ipc "github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// Event is a live event received from a remote instance's PUB stream.
type Event struct {
	// Topic is the ZMQ topic string (e.g. "message.append", "llm.token").
	Topic string
	// Payload contains the raw JSON payload of the event.
	Payload json.RawMessage
}

// RemoteSession subscribes to a remote instance's PUB stream and keeps a
// local mirror of the session state via an initial state.sync RPC bootstrap.
type RemoteSession struct {
	mu         sync.RWMutex
	instanceID string
	sessionID  string

	// pubEndpoint is the tcp:// address of the remote instance's PUB socket.
	pubEndpoint string
	// rpcEndpoint is the tcp:// address of the remote instance's ROUTER socket.
	rpcEndpoint string

	client *ipc.Client

	// events is the outbound channel exposed to callers via Messages().
	events chan Event

	// mirror holds the last known sessions from the most recent state.sync.
	mirror []protocol.SessionPayload
}

// NewRemoteSession creates a RemoteSession for the given instanceID and session.
// pubEndpoint and rpcEndpoint are tcp:// addresses (e.g. "tcp://127.0.0.1:5551").
// The caller must invoke Sync to bootstrap the initial state.
func NewRemoteSession(ctx context.Context, instanceID, sessionID, pubEndpoint, rpcEndpoint string) (*RemoteSession, error) {
	client, err := ipc.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("remoteview: new client: %w", err)
	}

	rs := &RemoteSession{
		instanceID:  instanceID,
		sessionID:   sessionID,
		pubEndpoint: pubEndpoint,
		rpcEndpoint: rpcEndpoint,
		client:      client,
		events:      make(chan Event, 128),
	}

	// Subscribe to all PUB topics and forward them on the events channel.
	envCh, err := client.SubscribeTo(pubEndpoint)
	if err != nil {
		return nil, fmt.Errorf("remoteview: subscribe: %w", err)
	}

	go func() {
		defer close(rs.events)
		for env := range envCh {
			select {
			case <-ctx.Done():
				return
			case rs.events <- Event{Topic: env.Topic, Payload: env.Payload}:
			}
		}
	}()

	return rs, nil
}

// InstanceID returns the remote instance identifier.
func (rs *RemoteSession) InstanceID() string { return rs.instanceID }

// SessionID returns the session identifier this view is tracking.
func (rs *RemoteSession) SessionID() string { return rs.sessionID }

// Messages returns a read-only channel that receives live events (message.append,
// llm.token, session.update, etc.) from the remote instance.
// The channel is closed when the context used in NewRemoteSession is cancelled.
func (rs *RemoteSession) Messages() <-chan Event { return rs.events }

// Sync calls state.sync on the remote instance via RPC and updates the local
// mirror. It must be called at least once before reading Mirror.
func (rs *RemoteSession) Sync(ctx context.Context) error {
	params := protocol.StateSyncParams{}
	raw, err := rs.client.Call(ctx, rs.rpcEndpoint, protocol.MethodStateSync, params)
	if err != nil {
		return fmt.Errorf("remoteview: state.sync: %w", err)
	}

	var result protocol.StateSyncResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("remoteview: state.sync: unmarshal result: %w", err)
	}

	rs.mu.Lock()
	rs.mirror = result.Sessions
	rs.mu.Unlock()

	return nil
}

// Mirror returns the cached list of sessions from the last Sync call.
func (rs *RemoteSession) Mirror() []protocol.SessionPayload {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	cp := make([]protocol.SessionPayload, len(rs.mirror))
	copy(cp, rs.mirror)
	return cp
}
