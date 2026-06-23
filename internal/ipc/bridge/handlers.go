// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// BusRegistrar is the minimal interface needed by RegisterHandlers to register RPC methods.
// *ipc.Bus satisfies this interface; a test double can also implement it.
type BusRegistrar interface {
	RegisterMethod(method string, handler ipc.HandlerFunc)
	Publish(topic string, payload any) error
}

// MessageRunner is the minimal interface used by the bridge to send a user message
// to a session via the local agent. A local interface is used to avoid import cycles
// between the bridge and the agent packages.
// RunMessage starts processing the given user message asynchronously and returns
// after the agent goroutine is launched. Any streaming events are handled internally.
type MessageRunner interface {
	RunMessage(ctx context.Context, sessionID string, content string) error
}

// SessionInterrupter is the minimal interface used by the bridge to cancel the
// running LLM call for a session. A local interface is used to avoid import cycles.
type SessionInterrupter interface {
	Cancel(sessionID string)
}

// DelegationRunner runs a delegated prompt in a fresh ephemeral session on the
// local agent and returns the captured assistant output synchronously. Local
// interface to avoid an import cycle with the agent package.
type DelegationRunner interface {
	RunDelegation(ctx context.Context, params protocol.DelegationRunParams) (protocol.DelegationRunResult, error)
	// CancelDelegation best-effort cancels an in-flight delegated run by the
	// caller-supplied correlation id. No-op if unknown.
	CancelDelegation(correlationID string)
}

// RegisterHandlers registers all JSON-RPC handlers on the Bus.
// instanceID is the local instance identifier (bus.instanceID is unexported).
// svc is the local session service; startedAt is when this instance started.
// runner and interrupter are optional: pass nil if message.send / session.interrupt
// should not be handled by this instance.
func RegisterHandlers(bus BusRegistrar, instanceID string, svc session.Service, msgSvc message.Service, startedAt time.Time) {
	RegisterHandlersWithAgent(bus, instanceID, svc, msgSvc, startedAt, nil, nil)
}

// RegisterHandlersWithAgent registers all JSON-RPC handlers including the agent-backed
// message.send and session.interrupt methods.
// Delegation is disabled; the ping handler advertises AcceptsDelegations=false.
func RegisterHandlersWithAgent(bus BusRegistrar, instanceID string, svc session.Service, msgSvc message.Service, startedAt time.Time, runner MessageRunner, interrupter SessionInterrupter) {
	RegisterHandlersWithDelegation(bus, instanceID, svc, msgSvc, startedAt, runner, interrupter, nil, false)
}

// RegisterHandlersWithDelegation registers all JSON-RPC handlers, with optional
// hot-peer delegation support. When acceptDelegations is true AND delRunner is
// non-nil the ping handler advertises AcceptsDelegations=true / DelegationProtocol=1
// and the delegation.run / delegation.cancel handlers are active.
func RegisterHandlersWithDelegation(bus BusRegistrar, instanceID string, svc session.Service, msgSvc message.Service, startedAt time.Time, runner MessageRunner, interrupter SessionInterrupter, delRunner DelegationRunner, acceptDelegations bool) {
	// Effective capability: only true when both flag and runner are present.
	accepts := acceptDelegations && delRunner != nil

	bus.RegisterMethod(protocol.MethodInstancePing, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		result := protocol.PingResult{
			Status:     "ok",
			InstanceID: instanceID,
			Uptime:     time.Since(startedAt).Round(time.Second).String(),
		}
		if accepts {
			result.AcceptsDelegations = true
			result.DelegationProtocol = protocol.DelegationProtocolVersion
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

	// message.send — triggers the local agent to process a user message in the given session.
	bus.RegisterMethod(protocol.MethodMessageSend, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.MessageSendParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("message.send: invalid params: %w", err)
		}
		if p.SessionID == "" {
			return nil, fmt.Errorf("message.send: session_id is required")
		}
		if p.Content == "" {
			return nil, fmt.Errorf("message.send: content is required")
		}
		if runner == nil {
			return nil, fmt.Errorf("message.send: agent runner not available on this instance")
		}
		if err := runner.RunMessage(ctx, p.SessionID, p.Content); err != nil {
			return nil, fmt.Errorf("message.send: run: %w", err)
		}
		return marshalResult(protocol.OKResult{OK: true})
	})

	// session.interrupt — cancels the active LLM run for the given session.
	bus.RegisterMethod(protocol.MethodSessionInterrupt, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.SessionInterruptParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("session.interrupt: invalid params: %w", err)
		}
		if p.SessionID == "" {
			return nil, fmt.Errorf("session.interrupt: session_id is required")
		}
		if interrupter == nil {
			return nil, fmt.Errorf("session.interrupt: agent interrupter not available on this instance")
		}
		interrupter.Cancel(p.SessionID)
		return marshalResult(protocol.OKResult{OK: true})
	})

	// message.list — returns the message history for a session.
	bus.RegisterMethod(protocol.MethodMessageList, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.MessageListParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("message.list: invalid params: %w", err)
		}
		if p.SessionID == "" {
			return nil, fmt.Errorf("message.list: session_id is required")
		}
		if msgSvc == nil {
			return nil, fmt.Errorf("message.list: message service not available on this instance")
		}
		msgs, err := msgSvc.List(ctx, p.SessionID)
		if err != nil {
			return nil, fmt.Errorf("message.list: %w", err)
		}
		payloads := make([]protocol.MessagePayload, 0, len(msgs))
		for _, msg := range msgs {
			payloads = append(payloads, messageToPayload(msg))
		}
		return marshalResult(payloads)
	})

	// delegation.run — runs a delegated prompt synchronously and returns the result.
	bus.RegisterMethod(protocol.MethodDelegationRun, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if !accepts {
			return nil, fmt.Errorf("delegation.run: this instance does not accept delegations")
		}
		var p protocol.DelegationRunParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("delegation.run: invalid params: %w", err)
		}
		if p.Prompt == "" {
			return nil, fmt.Errorf("delegation.run: prompt is required")
		}
		result, err := delRunner.RunDelegation(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("delegation.run: %w", err)
		}
		return marshalResult(result)
	})

	// delegation.cancel — best-effort cancels an in-flight delegated session by correlation id.
	bus.RegisterMethod(protocol.MethodDelegationCancel, func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		var p protocol.DelegationCancelParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("delegation.cancel: invalid params: %w", err)
		}
		if accepts && delRunner != nil {
			delRunner.CancelDelegation(p.CorrelationID)
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

// messageToPayload converts a message.Message to a protocol.MessagePayload.
// It extracts text content by joining all TextContent parts with newlines.
func messageToPayload(msg message.Message) protocol.MessagePayload {
	var parts []string
	for _, p := range msg.Parts {
		if tc, ok := p.(message.TextContent); ok && tc.Text != "" {
			parts = append(parts, tc.Text)
		}
	}
	return protocol.MessagePayload{
		ID:        msg.ID,
		Role:      string(msg.Role),
		Content:   strings.Join(parts, "\n"),
		CreatedAt: time.Unix(msg.CreatedAt, 0),
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
