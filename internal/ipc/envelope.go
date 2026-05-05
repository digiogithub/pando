// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"encoding/json"
	"time"
)

// Envelope is the standard wrapper for all ZMQ messages published over the PUB socket.
// Every message carries identity, routing, and timing metadata alongside the payload.
type Envelope struct {
	// InstanceID identifies the sending Pando instance.
	InstanceID string `json:"instanceId"`
	// ProjectID identifies the project associated with this event.
	ProjectID string `json:"projectId"`
	// SessionID identifies the session, if applicable.
	SessionID string `json:"sessionId,omitempty"`
	// Topic is the event topic used for subscriber filtering.
	Topic string `json:"topic"`
	// Timestamp records when this envelope was created.
	Timestamp time.Time `json:"timestamp"`
	// Payload is the raw JSON body of the event.
	Payload json.RawMessage `json:"payload"`
}
