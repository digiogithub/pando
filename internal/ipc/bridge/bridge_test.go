// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package bridge_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

// mockSessionService implements session.Service for testing.
type mockSessionService struct {
	*pubsub.Broker[session.Session]
	sessions []session.Session
}

func newMockSessionService(sessions []session.Session) *mockSessionService {
	return &mockSessionService{
		Broker:   pubsub.NewBroker[session.Session](),
		sessions: sessions,
	}
}

func (m *mockSessionService) Create(_ context.Context, _ string) (session.Session, error) {
	return session.Session{}, nil
}

func (m *mockSessionService) CreateTitleSession(_ context.Context, _ string) (session.Session, error) {
	return session.Session{}, nil
}

func (m *mockSessionService) CreateTaskSession(_ context.Context, _, _, _ string) (session.Session, error) {
	return session.Session{}, nil
}

func (m *mockSessionService) Get(_ context.Context, id string) (session.Session, error) {
	for _, s := range m.sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return session.Session{}, nil
}

func (m *mockSessionService) List(_ context.Context) ([]session.Session, error) {
	return m.sessions, nil
}

func (m *mockSessionService) Save(_ context.Context, s session.Session) (session.Session, error) {
	return s, nil
}

func (m *mockSessionService) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionService) EndSession(_ context.Context, _ string) error {
	return nil
}

// TestRegisterHandlers_InstancePing verifies that the instance.ping handler returns a valid PingResult.
func TestRegisterHandlers_InstancePing(t *testing.T) {
	svc := newMockSessionService(nil)
	startedAt := time.Now().Add(-5 * time.Minute) // simulate 5 minutes uptime

	captured := make(map[string]ipc.HandlerFunc)
	scratchBus := newCapturingBus(captured)
	bridge.RegisterHandlers(scratchBus, "test-instance-id", svc, nil, startedAt)

	// Now invoke the instance.ping handler directly from captured.
	pingHandler, ok := captured[protocol.MethodInstancePing]
	if !ok {
		t.Fatal("instance.ping handler not registered")
	}

	result, err := pingHandler(context.Background(), protocol.MethodInstancePing, nil)
	if err != nil {
		t.Fatalf("instance.ping handler returned error: %v", err)
	}

	var ping protocol.PingResult
	if err := json.Unmarshal(result, &ping); err != nil {
		t.Fatalf("unmarshal PingResult: %v", err)
	}

	if ping.Status != "ok" {
		t.Errorf("expected status %q, got %q", "ok", ping.Status)
	}
	if ping.InstanceID != "test-instance-id" {
		t.Errorf("expected instanceID %q, got %q", "test-instance-id", ping.InstanceID)
	}
	if ping.Uptime == "" {
		t.Error("expected non-empty uptime")
	}
}

// TestRegisterHandlers_SessionListEmpty verifies that session.list returns an empty slice when there are no sessions.
func TestRegisterHandlers_SessionListEmpty(t *testing.T) {
	svc := newMockSessionService(nil)
	startedAt := time.Now()

	captured := make(map[string]ipc.HandlerFunc)
	scratchBus := newCapturingBus(captured)
	bridge.RegisterHandlers(scratchBus, "test-instance-empty", svc, nil, startedAt)

	handler, ok := captured[protocol.MethodSessionList]
	if !ok {
		t.Fatal("session.list handler not registered")
	}

	result, err := handler(context.Background(), protocol.MethodSessionList, nil)
	if err != nil {
		t.Fatalf("session.list handler returned error: %v", err)
	}

	var sessions []protocol.SessionPayload
	if err := json.Unmarshal(result, &sessions); err != nil {
		t.Fatalf("unmarshal session list: %v", err)
	}

	if len(sessions) != 0 {
		t.Errorf("expected empty session list, got %d sessions", len(sessions))
	}
}

// TestRegisterHandlers_SessionListNonEmpty verifies that session.list returns the correct sessions.
func TestRegisterHandlers_SessionListNonEmpty(t *testing.T) {
	now := time.Now().Unix()
	mockSessions := []session.Session{
		{ID: "sess-1", Title: "First Session", MessageCount: 10, UpdatedAt: now},
		{ID: "sess-2", Title: "Second Session", MessageCount: 5, UpdatedAt: now},
	}

	svc := newMockSessionService(mockSessions)
	startedAt := time.Now()

	captured := make(map[string]ipc.HandlerFunc)
	scratchBus := newCapturingBus(captured)
	bridge.RegisterHandlers(scratchBus, "test-instance-list", svc, nil, startedAt)

	handler, ok := captured[protocol.MethodSessionList]
	if !ok {
		t.Fatal("session.list handler not registered")
	}

	result, err := handler(context.Background(), protocol.MethodSessionList, nil)
	if err != nil {
		t.Fatalf("session.list handler returned error: %v", err)
	}

	var sessions []protocol.SessionPayload
	if err := json.Unmarshal(result, &sessions); err != nil {
		t.Fatalf("unmarshal session list: %v", err)
	}

	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "sess-1" {
		t.Errorf("expected session ID %q, got %q", "sess-1", sessions[0].ID)
	}
	if sessions[1].Title != "Second Session" {
		t.Errorf("expected title %q, want %q", sessions[1].Title, "Second Session")
	}
}

// capturingBus is a test double for *ipc.Bus that captures registered handlers
// without requiring a real ZMQ connection.
type capturingBus struct {
	captured  map[string]ipc.HandlerFunc
	published []capturedPublish
}

type capturedPublish struct {
	topic   string
	payload any
}

func newCapturingBus(captured map[string]ipc.HandlerFunc) *capturingBus {
	return &capturingBus{captured: captured}
}

func (c *capturingBus) RegisterMethod(method string, handler ipc.HandlerFunc) {
	c.captured[method] = handler
}

func (c *capturingBus) Publish(topic string, payload any) error {
	c.published = append(c.published, capturedPublish{topic: topic, payload: payload})
	return nil
}
