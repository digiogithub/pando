// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package protocol

import "time"

// SessionPayload is published on session.update, session.activated, and session.deleted.
type SessionPayload struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int64     `json:"message_count"`
}

// SessionListPayload is published on session.list and carries the full session list.
type SessionListPayload struct {
	Sessions []SessionPayload `json:"sessions"`
}

// MessageAppendPayload is published on message.append.
type MessageAppendPayload struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Role      string `json:"role"`    // "user" or "assistant"
	Content   string `json:"content"` // text content or summary
}

// LLMTokenPayload is published on llm.token for each streaming token.
type LLMTokenPayload struct {
	SessionID string `json:"session_id"`
	Token     string `json:"token"`
}

// LLMStartPayload is published on llm.start when an LLM call begins.
type LLMStartPayload struct {
	SessionID string `json:"session_id"`
}

// LLMEndPayload is published on llm.end when an LLM call completes.
type LLMEndPayload struct {
	SessionID string `json:"session_id"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
}

// ToolStartPayload is published on tool.start when tool execution begins.
type ToolStartPayload struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	CallID    string `json:"call_id"`
	Params    string `json:"params"` // JSON params as string
}

// ToolEndPayload is published on tool.end when tool execution completes.
type ToolEndPayload struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	CallID    string `json:"call_id"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"` // short summary of result
}

// HeartbeatPayload is published on instance.heartbeat every 5 seconds.
type HeartbeatPayload struct {
	InstanceID      string    `json:"instance_id"`
	ActiveSessionID string    `json:"active_session_id,omitempty"`
	SessionCount    int       `json:"session_count"`
	Uptime          string    `json:"uptime"` // duration string e.g. "1h30m"
	StartedAt       time.Time `json:"started_at"`
}

// MessagePayload is a single message returned by message.list.
type MessagePayload struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`    // "user" or "assistant"
	Content   string    `json:"content"` // text representation of the message
	CreatedAt time.Time `json:"created_at"`
}

// ShutdownPayload is published on instance.shutdown during graceful shutdown.
type ShutdownPayload struct {
	InstanceID string `json:"instance_id"`
	Reason     string `json:"reason,omitempty"`
}
