// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/session"
)

// BusRegistrar is the minimal interface needed by RegisterHandlers to register RPC methods.
// *ipc.Bus satisfies this interface; a test double can also implement it.
type BusRegistrar interface {
	RegisterMethod(method string, handler ipc.HandlerFunc)
	Publish(topic string, payload any) error
}

// RegisterHandlers registers all JSON-RPC handlers on the Bus.
// instanceID is the local instance identifier (bus.instanceID is unexported).
// svc is the local session service; startedAt is when this instance started.
func RegisterHandlers(bus BusRegistrar, instanceID string, svc session.Service, startedAt time.Time) {
	bus.RegisterMethod(protocol.MethodInstancePing, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		result := protocol.PingResult{
			Status:     "ok",
			InstanceID: instanceID,
			Uptime:     time.Since(startedAt).Round(time.Second).String(),
		}
		return marshalResult(result)
	})

	bus.RegisterMethod(protocol.MethodSessionList, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		sessions, err := svc.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("session.list: %w", err)
		}
		payloads := make([]protocol.SessionPayload, len(sessions))
		for i, s := range sessions {
			payloads[i] = sessionToPayload(s)
		}
		return marshalResult(payloads)
	})

	bus.RegisterMethod(protocol.MethodSessionGet, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.SessionGetParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("session.get: invalid params: %w", err)
		}
		s, err := svc.Get(ctx, p.SessionID)
		if err != nil {
			return nil, fmt.Errorf("session.get: %w", err)
		}
		return marshalResult(sessionToPayload(s))
	})

	bus.RegisterMethod(protocol.MethodSessionActivate, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.SessionActivateParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("session.activate: invalid params: %w", err)
		}
		// Verify the session exists before publishing the activation event.
		s, err := svc.Get(ctx, p.SessionID)
		if err != nil {
			return nil, fmt.Errorf("session.activate: %w", err)
		}
		if err := bus.Publish(protocol.TopicSessionActivated, sessionToPayload(s)); err != nil {
			return nil, fmt.Errorf("session.activate: publish: %w", err)
		}
		return marshalResult(protocol.OKResult{OK: true})
	})
}

// sessionToPayload converts a session.Session to a protocol.SessionPayload.
func sessionToPayload(s session.Session) protocol.SessionPayload {
	return protocol.SessionPayload{
		ID:           s.ID,
		Title:        s.Title,
		UpdatedAt:    time.Unix(s.UpdatedAt, 0),
		MessageCount: s.MessageCount,
	}
}

// marshalResult marshals v to a json.RawMessage.
func marshalResult(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return b, nil
}
