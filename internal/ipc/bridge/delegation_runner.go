// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package bridge

import (
	"context"
	"fmt"
	"sync"

	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// delegationSessionCreator is the minimal session.Service capability used by
// agentDelegationRunner. A local interface is used so that test doubles do not
// have to implement the full session.Service.
type delegationSessionCreator interface {
	Create(ctx context.Context, title string) (session.Session, error)
}

// delegationAgentRunner is the minimal agent.Service capability used by
// agentDelegationRunner. A local interface is used so that test doubles do not
// have to implement the full agent.Service.
type delegationAgentRunner interface {
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan agent.AgentEvent, error)
	Cancel(sessionID string)
}

// agentDelegationRunner implements DelegationRunner using the local agent and
// session services. It creates a fresh ephemeral session per delegation run and
// tracks in-flight runs by correlation id so they can be cancelled.
type agentDelegationRunner struct {
	sessions delegationSessionCreator
	agentSvc delegationAgentRunner

	mu sync.Mutex
	// inflight maps caller-supplied correlation id → ephemeral session id while
	// the delegation.run RPC is executing. Entries are removed on return.
	inflight map[string]string
}

// NewAgentDelegationRunner returns a DelegationRunner backed by the local agent
// and session services. Both deps must be non-nil; NewAgentDelegationRunner
// panics if either is nil.
func NewAgentDelegationRunner(sessions session.Service, agentSvc agent.Service) DelegationRunner {
	if sessions == nil {
		panic("bridge: NewAgentDelegationRunner: sessions must not be nil")
	}
	if agentSvc == nil {
		panic("bridge: NewAgentDelegationRunner: agentSvc must not be nil")
	}
	return newAgentDelegationRunnerFromDeps(sessions, agentSvc)
}

// newAgentDelegationRunnerFromDeps constructs a runner from the narrow local
// interfaces. It is used by tests without requiring the full service types.
func newAgentDelegationRunnerFromDeps(sessions delegationSessionCreator, agentSvc delegationAgentRunner) DelegationRunner {
	return &agentDelegationRunner{
		sessions: sessions,
		agentSvc: agentSvc,
		inflight: make(map[string]string),
	}
}

// RunDelegation creates a fresh ephemeral session, runs the delegated prompt
// synchronously, and returns the captured assistant output. The session is kept
// after the run completes for history/recovery; it is not deleted.
//
// The caller-supplied CorrelationID is tracked while the run is in-flight so
// that CancelDelegation can interrupt it. The mapping is removed on return.
func (r *agentDelegationRunner) RunDelegation(ctx context.Context, params protocol.DelegationRunParams) (protocol.DelegationRunResult, error) {
	if params.Prompt == "" {
		return protocol.DelegationRunResult{}, fmt.Errorf("delegated run: prompt must not be empty")
	}

	// Build a human-readable title for the ephemeral session.
	title := "delegated task"
	if params.CorrelationID != "" {
		short := params.CorrelationID
		if len(short) > 12 {
			short = short[:12]
		}
		title = "delegated: " + short
	}

	sess, err := r.sessions.Create(ctx, title)
	if err != nil {
		return protocol.DelegationRunResult{}, fmt.Errorf("delegated run: create session: %w", err)
	}

	// Register the correlation id → session id mapping for the duration of this
	// run so CancelDelegation can find the live session.
	if params.CorrelationID != "" {
		r.mu.Lock()
		r.inflight[params.CorrelationID] = sess.ID
		r.mu.Unlock()

		defer func() {
			r.mu.Lock()
			delete(r.inflight, params.CorrelationID)
			r.mu.Unlock()
		}()
	}

	// TODO(B3-followup): apply per-session persona/model override from
	// params.Persona and params.Model before calling agentSvc.Run.

	done, err := r.agentSvc.Run(ctx, sess.ID, params.Prompt)
	if err != nil {
		return protocol.DelegationRunResult{}, fmt.Errorf("delegated run: %w", err)
	}

	result := <-done
	if result.Error != nil {
		return protocol.DelegationRunResult{}, fmt.Errorf("delegated run: %w", result.Error)
	}
	if result.Message.Role != message.Assistant {
		return protocol.DelegationRunResult{}, fmt.Errorf("delegated run produced no assistant response")
	}

	return protocol.DelegationRunResult{
		SessionID:  sess.ID,
		Output:     result.Message.Content().String(),
		StopReason: "end_turn",
	}, nil
}

// CancelDelegation best-effort cancels the in-flight delegated session keyed by
// correlationID. It is a no-op if the correlation id is unknown or the run has
// already completed.
func (r *agentDelegationRunner) CancelDelegation(correlationID string) {
	if correlationID == "" {
		return
	}
	r.mu.Lock()
	sessionID, ok := r.inflight[correlationID]
	r.mu.Unlock()
	if ok {
		r.agentSvc.Cancel(sessionID)
	}
}
