// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package remoteview

import (
	"context"
	"encoding/json"
	"fmt"

	ipc "github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
)

// RemoteControl wraps an ipc.Client with typed methods for controlling a remote
// Pando instance over the ROUTER/DEALER JSON-RPC channel.
type RemoteControl struct {
	client      *ipc.Client
	rpcEndpoint string
}

// NewRemoteControl creates a RemoteControl that sends RPC calls to rpcEndpoint
// (e.g. "tcp://127.0.0.1:5552").
func NewRemoteControl(ctx context.Context, rpcEndpoint string) (*RemoteControl, error) {
	client, err := ipc.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("remoteview: new control client: %w", err)
	}
	return &RemoteControl{
		client:      client,
		rpcEndpoint: rpcEndpoint,
	}, nil
}

// SendMessage sends a user message to the specified session on the remote instance.
func (rc *RemoteControl) SendMessage(ctx context.Context, sessionID, content string) error {
	params := protocol.MessageSendParams{
		SessionID: sessionID,
		Content:   content,
	}
	_, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodMessageSend, params)
	if err != nil {
		return fmt.Errorf("remoteview: message.send: %w", err)
	}
	return nil
}

// SwitchSession activates the given sessionID on the remote instance.
func (rc *RemoteControl) SwitchSession(ctx context.Context, sessionID string) error {
	params := protocol.SessionActivateParams{SessionID: sessionID}
	_, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodSessionActivate, params)
	if err != nil {
		return fmt.Errorf("remoteview: session.activate: %w", err)
	}
	return nil
}

// Interrupt cancels the running LLM call for the given session on the remote instance.
func (rc *RemoteControl) Interrupt(ctx context.Context, sessionID string) error {
	params := protocol.SessionInterruptParams{SessionID: sessionID}
	_, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodSessionInterrupt, params)
	if err != nil {
		return fmt.Errorf("remoteview: session.interrupt: %w", err)
	}
	return nil
}

// ListSessions returns the list of sessions on the remote instance.
func (rc *RemoteControl) ListSessions(ctx context.Context) ([]protocol.SessionPayload, error) {
	raw, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodSessionList, struct{}{})
	if err != nil {
		return nil, fmt.Errorf("remoteview: session.list: %w", err)
	}
	var sessions []protocol.SessionPayload
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil, fmt.Errorf("remoteview: session.list: unmarshal: %w", err)
	}
	return sessions, nil
}

// GetSession returns details for a single session on the remote instance.
func (rc *RemoteControl) GetSession(ctx context.Context, sessionID string) (protocol.SessionPayload, error) {
	params := protocol.SessionGetParams{SessionID: sessionID}
	raw, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodSessionGet, params)
	if err != nil {
		return protocol.SessionPayload{}, fmt.Errorf("remoteview: session.get: %w", err)
	}
	var sess protocol.SessionPayload
	if err := json.Unmarshal(raw, &sess); err != nil {
		return protocol.SessionPayload{}, fmt.Errorf("remoteview: session.get: unmarshal: %w", err)
	}
	return sess, nil
}

// ListMessages returns the message history for the given session on the remote instance.
func (rc *RemoteControl) ListMessages(ctx context.Context, sessionID string) ([]protocol.MessagePayload, error) {
	params := protocol.MessageListParams{SessionID: sessionID}
	raw, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodMessageList, params)
	if err != nil {
		return nil, fmt.Errorf("remoteview: message.list: %w", err)
	}
	var msgs []protocol.MessagePayload
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, fmt.Errorf("remoteview: message.list: unmarshal: %w", err)
	}
	return msgs, nil
}

// Ping checks that the remote instance is alive and returns its status.
func (rc *RemoteControl) Ping(ctx context.Context) (protocol.PingResult, error) {
	raw, err := rc.client.Call(ctx, rc.rpcEndpoint, protocol.MethodInstancePing, struct{}{})
	if err != nil {
		return protocol.PingResult{}, fmt.Errorf("remoteview: instance.ping: %w", err)
	}
	var result protocol.PingResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocol.PingResult{}, fmt.Errorf("remoteview: instance.ping: unmarshal: %w", err)
	}
	return result, nil
}
