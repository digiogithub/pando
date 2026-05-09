// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package protocol

// RPC method name constants used for JSON-RPC 2.0 calls over the ROUTER socket.
const (
	// MethodStateSync requests a full state snapshot from the primary instance.
	MethodStateSync = "state.sync"
	// MethodSessionList requests the full list of sessions.
	MethodSessionList = "session.list"
	// MethodSessionGet requests a single session by ID.
	MethodSessionGet = "session.get"
	// MethodSessionActivate requests changing the active session.
	MethodSessionActivate = "session.activate"
	// MethodMessageSend sends a user message to a session.
	MethodMessageSend = "message.send"
	// MethodSessionInterrupt cancels the running LLM call for a session.
	MethodSessionInterrupt = "session.interrupt"
	// MethodInstancePing checks that the instance is alive.
	MethodInstancePing = "instance.ping"
	// MethodInstanceInfo retrieves detailed instance information.
	MethodInstanceInfo = "instance.info"
	// MethodMessageList requests the message history for a session.
	MethodMessageList = "message.list"
)

// StateSyncParams is the parameter struct for the state.sync RPC method.
type StateSyncParams struct {
	ProjectID string `json:"project_id,omitempty"`
}

// StateSyncResult is the response for state.sync.
type StateSyncResult struct {
	Sessions        []SessionPayload `json:"sessions"`
	ActiveSessionID string           `json:"active_session_id,omitempty"`
	InstanceID      string           `json:"instance_id"`
}

// SessionGetParams is the parameter struct for session.get.
type SessionGetParams struct {
	SessionID string `json:"session_id"`
}

// SessionActivateParams is the parameter struct for session.activate.
type SessionActivateParams struct {
	SessionID string `json:"session_id"`
}

// MessageSendParams is the parameter struct for message.send.
type MessageSendParams struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

// SessionInterruptParams is the parameter struct for session.interrupt.
type SessionInterruptParams struct {
	SessionID string `json:"session_id"`
}

// MessageListParams is the parameter struct for message.list.
type MessageListParams struct {
	SessionID string `json:"session_id"`
}

// OKResult is a generic success response.
type OKResult struct {
	OK bool `json:"ok"`
}

// PingResult is the response for instance.ping.
type PingResult struct {
	Status     string `json:"status"`
	InstanceID string `json:"instance_id"`
	Uptime     string `json:"uptime"`
}
