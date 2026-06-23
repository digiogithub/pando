// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Internal-package tests for agentDelegationRunner. Being in package bridge
// (not bridge_test) lets us use newAgentDelegationRunnerFromDeps with narrow
// local stubs instead of having to satisfy the full agent.Service and
// session.Service interfaces.

package bridge

import (
	"context"
	"fmt"
	"testing"

	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// --- minimal stubs for delegationSessionCreator ---

type stubSessionCreator struct {
	nextID    string
	created   []string
	createErr error
}

func (s *stubSessionCreator) Create(_ context.Context, title string) (session.Session, error) {
	if s.createErr != nil {
		return session.Session{}, s.createErr
	}
	s.created = append(s.created, title)
	return session.Session{ID: s.nextID, Title: title}, nil
}

// --- minimal stubs for delegationAgentRunner ---

type stubAgentRunner struct {
	// result is the single AgentEvent written to the channel on Run.
	result    agent.AgentEvent
	runErr    error
	cancelled []string
}

func (s *stubAgentRunner) Run(_ context.Context, _ string, _ string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent, 1)
	if s.runErr == nil {
		ch <- s.result
	}
	close(ch)
	return ch, s.runErr
}

func (s *stubAgentRunner) Cancel(sessionID string) {
	s.cancelled = append(s.cancelled, sessionID)
}

// --- tests ---

func TestAgentDelegationRunner_EmptyPromptRejected(t *testing.T) {
	sess := &stubSessionCreator{nextID: "s1"}
	r := newAgentDelegationRunnerFromDeps(sess, &stubAgentRunner{})
	_, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{Prompt: ""})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
	if len(sess.created) != 0 {
		t.Errorf("expected no session created for empty prompt, got %d", len(sess.created))
	}
}

func TestAgentDelegationRunner_SessionCreateError(t *testing.T) {
	sess := &stubSessionCreator{createErr: fmt.Errorf("db unavailable")}
	r := newAgentDelegationRunnerFromDeps(sess, &stubAgentRunner{})
	_, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{Prompt: "hello"})
	if err == nil {
		t.Fatal("expected error when session create fails")
	}
}

func TestAgentDelegationRunner_SuccessfulRun(t *testing.T) {
	const wantOutput = "all done"
	const sessionID = "sess-ok"

	sess := &stubSessionCreator{nextID: sessionID}
	agSvc := &stubAgentRunner{
		result: agent.AgentEvent{
			Type: agent.AgentEventTypeResponse,
			Message: message.Message{
				Role:  message.Assistant,
				Parts: []message.ContentPart{message.TextContent{Text: wantOutput}},
			},
		},
	}

	r := newAgentDelegationRunnerFromDeps(sess, agSvc)
	result, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{
		Prompt:        "do the thing",
		CorrelationID: "corr-123",
	})
	if err != nil {
		t.Fatalf("RunDelegation: %v", err)
	}
	if result.Output != wantOutput {
		t.Errorf("output: got %q, want %q", result.Output, wantOutput)
	}
	if result.SessionID != sessionID {
		t.Errorf("session_id: got %q, want %q", result.SessionID, sessionID)
	}
	if result.StopReason != "end_turn" {
		t.Errorf("stop_reason: got %q, want %q", result.StopReason, "end_turn")
	}
	if len(sess.created) != 1 {
		t.Errorf("expected 1 created session, got %d", len(sess.created))
	}
}

func TestAgentDelegationRunner_NonAssistantResponseError(t *testing.T) {
	sess := &stubSessionCreator{nextID: "s1"}
	agSvc := &stubAgentRunner{
		result: agent.AgentEvent{
			Type:    agent.AgentEventTypeResponse,
			Message: message.Message{Role: message.User}, // wrong role
		},
	}
	r := newAgentDelegationRunnerFromDeps(sess, agSvc)
	_, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{Prompt: "go"})
	if err == nil {
		t.Fatal("expected error for non-assistant response")
	}
}

func TestAgentDelegationRunner_AgentRunError(t *testing.T) {
	sess := &stubSessionCreator{nextID: "s1"}
	agSvc := &stubAgentRunner{runErr: fmt.Errorf("provider down")}
	r := newAgentDelegationRunnerFromDeps(sess, agSvc)
	_, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{Prompt: "go"})
	if err == nil {
		t.Fatal("expected error when agent.Run fails")
	}
}

func TestAgentDelegationRunner_CancelInflightEntry(t *testing.T) {
	const corrID = "corr-xyz"
	const sessID = "sess-live"

	agSvc := &stubAgentRunner{}
	// Seed the inflight map manually (simulates a concurrent RunDelegation).
	inner := &agentDelegationRunner{
		inflight: map[string]string{corrID: sessID},
		agentSvc: agSvc,
	}

	inner.CancelDelegation(corrID)
	if len(agSvc.cancelled) != 1 || agSvc.cancelled[0] != sessID {
		t.Errorf("expected Cancel(%q), got %v", sessID, agSvc.cancelled)
	}
}

func TestAgentDelegationRunner_CancelUnknownID_NoOp(t *testing.T) {
	agSvc := &stubAgentRunner{}
	inner := &agentDelegationRunner{
		inflight: make(map[string]string),
		agentSvc: agSvc,
	}
	// Must not panic and must not call Cancel.
	inner.CancelDelegation("never-seen")
	if len(agSvc.cancelled) != 0 {
		t.Errorf("expected no Cancel for unknown id, got %v", agSvc.cancelled)
	}
}

func TestAgentDelegationRunner_InflightRemovedAfterRun(t *testing.T) {
	const corrID = "corr-cleanup"
	sess := &stubSessionCreator{nextID: "s2"}
	agSvc := &stubAgentRunner{
		result: agent.AgentEvent{
			Type: agent.AgentEventTypeResponse,
			Message: message.Message{
				Role:  message.Assistant,
				Parts: []message.ContentPart{message.TextContent{Text: "ok"}},
			},
		},
	}

	r := newAgentDelegationRunnerFromDeps(sess, agSvc)
	_, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{
		Prompt:        "work",
		CorrelationID: corrID,
	})
	if err != nil {
		t.Fatalf("RunDelegation: %v", err)
	}

	// After the run the inflight entry must be gone; Cancel must be a no-op.
	r.CancelDelegation(corrID)
	if len(agSvc.cancelled) != 0 {
		t.Errorf("expected no Cancel after run completed, got %v", agSvc.cancelled)
	}
}
