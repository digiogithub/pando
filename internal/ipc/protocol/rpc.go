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
	// MethodDelegationRun runs a delegated prompt in a fresh ephemeral session on
	// the target instance and returns the captured assistant output synchronously
	// (hot-peer delegation, B3). Only handled when the target opted in via
	// AcceptDelegations; otherwise the handler returns a typed refusal.
	MethodDelegationRun = "delegation.run"
	// MethodDelegationCancel best-effort cancels an in-flight delegated session on
	// the target by its session id (the caller uses this on ctx cancellation).
	MethodDelegationCancel = "delegation.cancel"
)

// DelegationProtocolVersion is the wire version of the delegation.run contract.
// A caller must observe a peer advertising DelegationProtocol >= this value (via
// PingResult) before routing a delegation to it; capability negotiation fails
// closed so a peer that is too old (advertising 0) is simply never used.
const DelegationProtocolVersion = 1

// DelegationRunParams is the parameter struct for delegation.run. The software
// owns all launch metadata; the caller passes only the rendered prompt plus the
// optional persona/model overrides and the correlation id used for idempotency.
type DelegationRunParams struct {
	Prompt        string `json:"prompt"`
	Cwd           string `json:"cwd,omitempty"`
	Persona       string `json:"persona,omitempty"`
	Model         string `json:"model,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

// DelegationRunResult is the response for delegation.run. Output is the full
// assistant message text for the turn (the caller feeds it to conclusion.Enrich
// to extract the <pando:conclusion> block); SessionID is the ephemeral session
// created on the target for correlation/recovery.
type DelegationRunResult struct {
	SessionID  string `json:"session_id"`
	Output     string `json:"output"`
	StopReason string `json:"stop_reason,omitempty"`
}

// DelegationCancelParams is the parameter struct for delegation.cancel. It keys
// on the caller-owned CorrelationID rather than the target's session id, because
// the synchronous delegation.run RPC only returns the session id AFTER the turn
// completes — so a caller cancelling an in-flight delegation has only the
// correlation id it supplied in DelegationRunParams. The target maps the
// correlation id to the live ephemeral session while the run is in flight.
type DelegationCancelParams struct {
	CorrelationID string `json:"correlation_id"`
}

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

// PingResult is the response for instance.ping. It doubles as the delegation
// capability advertisement: AcceptsDelegations + DelegationProtocol let a caller
// learn, in the same round-trip that confirms liveness, whether this peer will
// accept a delegation.run and at which wire version. Older peers omit these
// fields (false/0), so capability negotiation fails closed.
type PingResult struct {
	Status     string `json:"status"`
	InstanceID string `json:"instance_id"`
	Uptime     string `json:"uptime"`
	// AcceptsDelegations is true when this instance opted in (AcceptDelegations)
	// and therefore handles delegation.run.
	AcceptsDelegations bool `json:"accepts_delegations,omitempty"`
	// DelegationProtocol is the wire version of the delegation.run contract this
	// instance speaks; 0/absent means "does not accept delegations".
	DelegationProtocol int `json:"delegation_protocol,omitempty"`
}
