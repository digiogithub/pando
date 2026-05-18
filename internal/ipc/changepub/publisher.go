// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package changepub publishes write-change events on the IPC PUB socket after
// every successful DB write on the primary instance, allowing secondaries to
// invalidate caches and refresh views without polling.
package changepub

import (
	"context"
	"encoding/json"
	"time"
)

// Publisher sends write-change events on the IPC bus after every DB write.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// WriteChange is the standard envelope for write-change events.
type WriteChange struct {
	InstanceID string          `json:"instance_id"`
	Path       string          `json:"path"`
	Topic      string          `json:"topic"`
	Payload    json.RawMessage `json:"payload"`
	Timestamp  string          `json:"timestamp"` // RFC3339
}

// PublishFunc is the signature of the bus publish function.
type PublishFunc func(topic string, payload any) error

// BusPublisher publishes write-change events through an ipc.Bus.
type BusPublisher struct {
	publishFn  PublishFunc
	instanceID string
	path       string
}

// NewBusPublisher creates a BusPublisher that wraps the given bus publish function.
func NewBusPublisher(publishFn PublishFunc, instanceID, path string) *BusPublisher {
	return &BusPublisher{
		publishFn:  publishFn,
		instanceID: instanceID,
		path:       path,
	}
}

// Publish wraps payload in a WriteChange envelope and sends it on the bus.
// The context parameter is reserved for future cancellation support.
func (p *BusPublisher) Publish(_ context.Context, topic string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	change := WriteChange{
		InstanceID: p.instanceID,
		Path:       p.path,
		Topic:      topic,
		Payload:    raw,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
	return p.publishFn(topic, change)
}

// NoopPublisher discards all events — used when no bus is available.
type NoopPublisher struct{}

// Publish is a no-op that always succeeds.
func (NoopPublisher) Publish(_ context.Context, _ string, _ any) error { return nil }
