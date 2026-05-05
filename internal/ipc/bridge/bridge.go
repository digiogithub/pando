// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package bridge connects in-process pubsub events to the ZMQ Bus, enabling
// real-time event broadcasting to all connected Pando instances and observers.
package bridge

import (
	"context"
	"log"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

// Bridge connects the in-process pubsub events to the ZMQ Bus.
// Only the primary instance should create a Bridge.
type Bridge struct {
	bus       *ipc.Bus
	sessions  session.Service
	agentSvc  agent.Service
	startedAt time.Time
}

// New creates a Bridge that forwards session and agent events to the ZMQ Bus.
func New(bus *ipc.Bus, sessions session.Service, agentSvc agent.Service) *Bridge {
	return &Bridge{
		bus:       bus,
		sessions:  sessions,
		agentSvc:  agentSvc,
		startedAt: time.Now(),
	}
}

// Start begins bridging events. It subscribes to session and agent pubsub events
// and starts the heartbeat goroutine. It runs until ctx is cancelled.
func (b *Bridge) Start(ctx context.Context) {
	go b.bridgeSessions(ctx)
	go b.bridgeAgent(ctx)
	go b.runHeartbeat(ctx)
}

// bridgeSessions subscribes to session pubsub events and forwards them to the ZMQ Bus.
func (b *Bridge) bridgeSessions(ctx context.Context) {
	ch := b.sessions.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b.handleSessionEvent(ev)
		}
	}
}

func (b *Bridge) handleSessionEvent(ev pubsub.Event[session.Session]) {
	s := ev.Payload
	payload := protocol.SessionPayload{
		ID:           s.ID,
		Title:        s.Title,
		UpdatedAt:    time.Unix(s.UpdatedAt, 0),
		MessageCount: s.MessageCount,
	}

	var topic string
	switch ev.Type {
	case pubsub.CreatedEvent, pubsub.UpdatedEvent:
		topic = protocol.TopicSessionUpdate
	case pubsub.DeletedEvent:
		topic = protocol.TopicSessionDeleted
	default:
		return
	}

	if err := b.bus.Publish(topic, payload); err != nil {
		log.Printf("ipc: bridge: publish %s: %v", topic, err)
	}
}

// bridgeAgent subscribes to agent pubsub events and forwards them to the ZMQ Bus.
func (b *Bridge) bridgeAgent(ctx context.Context) {
	ch := b.agentSvc.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b.handleAgentEvent(ev)
		}
	}
}

func (b *Bridge) handleAgentEvent(ev pubsub.Event[agent.AgentEvent]) {
	ae := ev.Payload
	switch ae.Type {
	case agent.AgentEventTypeContentDelta:
		payload := protocol.LLMTokenPayload{
			SessionID: ae.SessionID,
			Token:     ae.Delta,
		}
		if err := b.bus.Publish(protocol.TopicLLMToken, payload); err != nil {
			log.Printf("ipc: bridge: publish %s: %v", protocol.TopicLLMToken, err)
		}

	case agent.AgentEventTypeToolCall:
		if ae.ToolCall == nil {
			return
		}
		payload := protocol.ToolStartPayload{
			SessionID: ae.SessionID,
			ToolName:  ae.ToolCall.Name,
			CallID:    ae.ToolCall.ID,
			Params:    ae.ToolCall.Input,
		}
		if err := b.bus.Publish(protocol.TopicToolStart, payload); err != nil {
			log.Printf("ipc: bridge: publish %s: %v", protocol.TopicToolStart, err)
		}

	case agent.AgentEventTypeToolResult:
		if ae.ToolResult == nil {
			return
		}
		// Truncate result content to a reasonable length for the event payload.
		result := ae.ToolResult.Content
		if len(result) > 512 {
			result = result[:512] + "..."
		}
		payload := protocol.ToolEndPayload{
			SessionID: ae.SessionID,
			ToolName:  ae.ToolResult.Name,
			CallID:    ae.ToolResult.ToolCallID,
			IsError:   ae.ToolResult.IsError,
			Result:    result,
		}
		if err := b.bus.Publish(protocol.TopicToolEnd, payload); err != nil {
			log.Printf("ipc: bridge: publish %s: %v", protocol.TopicToolEnd, err)
		}

	case agent.AgentEventTypeResponse:
		payload := protocol.LLMEndPayload{
			SessionID: ae.SessionID,
		}
		if err := b.bus.Publish(protocol.TopicLLMEnd, payload); err != nil {
			log.Printf("ipc: bridge: publish %s: %v", protocol.TopicLLMEnd, err)
		}
	}
}

// runHeartbeat publishes a HeartbeatPayload every 5 seconds until ctx is cancelled.
func (b *Bridge) runHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			uptime := time.Since(b.startedAt).Round(time.Second).String()
			payload := protocol.HeartbeatPayload{
				InstanceID: b.bus.PubAddr, // PubAddr is the only exported identifier
				Uptime:     uptime,
				StartedAt:  b.startedAt,
			}
			if err := b.bus.Publish(protocol.TopicInstanceHeartbeat, payload); err != nil {
				log.Printf("ipc: bridge: publish %s: %v", protocol.TopicInstanceHeartbeat, err)
			}
		}
	}
}
