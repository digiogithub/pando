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
	"time"

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

// TestDelegationSessionOverrides covers the mapping from a delegation request's
// persona/model to a per-session LLM override (B3 follow-up): a requested model
// becomes the override Model, a requested persona becomes a PersonaScoped
// override, neither set means ok=false (target keeps its defaults).
func TestDelegationSessionOverrides(t *testing.T) {
	cases := []struct {
		name        string
		params      protocol.DelegationRunParams
		wantOK      bool
		wantModel   string
		wantPersona string
		wantScoped  bool
	}{
		{name: "none", params: protocol.DelegationRunParams{Prompt: "x"}, wantOK: false},
		{name: "model only", params: protocol.DelegationRunParams{Model: "anthropic.claude-opus-4-8"},
			wantOK: true, wantModel: "anthropic.claude-opus-4-8"},
		{name: "persona only", params: protocol.DelegationRunParams{Persona: "qa"},
			wantOK: true, wantPersona: "qa", wantScoped: true},
		{name: "both", params: protocol.DelegationRunParams{Model: "gpt-5.4", Persona: "software-engineer"},
			wantOK: true, wantModel: "gpt-5.4", wantPersona: "software-engineer", wantScoped: true},
		{name: "blank trimmed", params: protocol.DelegationRunParams{Model: "  ", Persona: "  "}, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ov, ok := delegationSessionOverrides(tc.params)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if string(ov.Model) != tc.wantModel {
				t.Errorf("model = %q, want %q", ov.Model, tc.wantModel)
			}
			if ov.Persona != tc.wantPersona {
				t.Errorf("persona = %q, want %q", ov.Persona, tc.wantPersona)
			}
			if ov.PersonaScoped != tc.wantScoped {
				t.Errorf("personaScoped = %v, want %v", ov.PersonaScoped, tc.wantScoped)
			}
		})
	}
}

// TestAgentDelegationRunner_RunWithOverrides exercises the apply-then-clear
// override path (B3 follow-up): a run carrying a persona/model override completes
// normally. The override application/isolation semantics themselves are covered
// by the agent package's per-session concurrency tests; here we assert the
// delegation runner threads them without disrupting a successful run.
func TestAgentDelegationRunner_RunWithOverrides(t *testing.T) {
	const sessionID = "sess-ov"
	const wantOutput = "done with overrides"
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
	res, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{
		Prompt:        "work",
		Model:         "anthropic.claude-opus-4-8",
		Persona:       "qa",
		CorrelationID: "corr-ov",
	})
	if err != nil {
		t.Fatalf("RunDelegation: %v", err)
	}
	if res.Output != wantOutput {
		t.Errorf("output: got %q, want %q", res.Output, wantOutput)
	}
	if res.SessionID != sessionID {
		t.Errorf("session_id: got %q, want %q", res.SessionID, sessionID)
	}
}

// --- delegation.status / result cache tests (A2) ---

// TestDelegationStatus_Lifecycle: a completed run is recoverable via
// DelegationStatus (completed + cached payload); an unrelated id is unknown.
func TestDelegationStatus_Lifecycle(t *testing.T) {
	const sessionID = "sess-st"
	const wantOutput = "the result"
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
	r := &agentDelegationRunner{
		sessions: sess,
		agentSvc: agSvc,
		inflight: map[string]string{},
		results:  map[string]delegationResultEntry{},
	}

	// Unknown before any run.
	if got := r.DelegationStatus("corr-st"); got.State != protocol.DelegationStateUnknown {
		t.Fatalf("pre-run state = %q, want unknown", got.State)
	}

	if _, err := r.RunDelegation(context.Background(), protocol.DelegationRunParams{
		Prompt:        "go",
		CorrelationID: "corr-st",
	}); err != nil {
		t.Fatalf("RunDelegation: %v", err)
	}

	got := r.DelegationStatus("corr-st")
	if got.State != protocol.DelegationStateCompleted {
		t.Fatalf("post-run state = %q, want completed", got.State)
	}
	if got.Output != wantOutput {
		t.Errorf("output = %q, want %q", got.Output, wantOutput)
	}
	if got.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", got.SessionID, sessionID)
	}

	// A stranger correlation id is unknown.
	if got := r.DelegationStatus("nope"); got.State != protocol.DelegationStateUnknown {
		t.Errorf("stranger state = %q, want unknown", got.State)
	}
	// Blank id is unknown (not cacheable).
	if got := r.DelegationStatus(""); got.State != protocol.DelegationStateUnknown {
		t.Errorf("blank state = %q, want unknown", got.State)
	}
}

// TestDelegationStatus_RunningWhileInflight: an entry present in the inflight map
// reports running (inflight takes precedence over any cached result).
func TestDelegationStatus_RunningWhileInflight(t *testing.T) {
	r := &agentDelegationRunner{
		inflight: map[string]string{"corr-run": "s1"},
		results:  map[string]delegationResultEntry{},
	}
	if got := r.DelegationStatus("corr-run"); got.State != protocol.DelegationStateRunning {
		t.Errorf("state = %q, want running", got.State)
	}
}

// TestDelegationResultCache_TTLExpiry: a cached result past its TTL reports unknown.
func TestDelegationResultCache_TTLExpiry(t *testing.T) {
	base := time.Now()
	cur := base
	r := &agentDelegationRunner{
		inflight: map[string]string{},
		results:  map[string]delegationResultEntry{},
		nowFn:    func() time.Time { return cur },
	}
	r.storeResult("corr-ttl", protocol.DelegationRunResult{Output: "x"})

	if got := r.DelegationStatus("corr-ttl"); got.State != protocol.DelegationStateCompleted {
		t.Fatalf("within TTL state = %q, want completed", got.State)
	}
	cur = base.Add(delegationResultTTL + time.Minute)
	if got := r.DelegationStatus("corr-ttl"); got.State != protocol.DelegationStateUnknown {
		t.Errorf("past TTL state = %q, want unknown", got.State)
	}
}

// TestDelegationResultCache_CapEviction: storing beyond the cap evicts the oldest
// entries, keeping the cache bounded and the most recent recoverable.
func TestDelegationResultCache_CapEviction(t *testing.T) {
	base := time.Now()
	cur := base
	r := &agentDelegationRunner{
		inflight: map[string]string{},
		results:  map[string]delegationResultEntry{},
		nowFn:    func() time.Time { return cur },
	}
	total := delegationResultCacheMax + 5
	for i := 0; i < total; i++ {
		cur = base.Add(time.Duration(i) * time.Second)
		r.storeResult(fmt.Sprintf("corr-%d", i), protocol.DelegationRunResult{Output: "y"})
	}

	if len(r.results) > delegationResultCacheMax {
		t.Errorf("cache size = %d, want <= %d", len(r.results), delegationResultCacheMax)
	}
	// The oldest entry (corr-0) must have been evicted.
	if got := r.DelegationStatus("corr-0"); got.State != protocol.DelegationStateUnknown {
		t.Errorf("oldest entry state = %q, want unknown (evicted)", got.State)
	}
	// The most recent entry survives.
	if got := r.DelegationStatus(fmt.Sprintf("corr-%d", total-1)); got.State != protocol.DelegationStateCompleted {
		t.Errorf("newest entry state = %q, want completed", got.State)
	}
}
