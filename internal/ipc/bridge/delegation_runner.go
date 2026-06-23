// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package bridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// Bounds for the per-runner completed-delegation result cache that backs
// delegation.status (external re-attach, A2). The cache lets a caller that
// restarted mid-delegation recover a result it can no longer hold a blocking
// delegation.run open for. It is intentionally small and short-lived: recovery
// happens within seconds of a restart, so a few recent results suffice.
const (
	delegationResultCacheMax = 64
	delegationResultTTL      = 30 * time.Minute
)

// delegationResultEntry is a completed delegation retained in the result cache.
type delegationResultEntry struct {
	result      protocol.DelegationRunResult
	completedAt time.Time
}

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
	// results retains recently completed delegations by correlation id so
	// delegation.status can report a result to a caller that restarted after the
	// blocking delegation.run returned (external re-attach, A2). Bounded by
	// delegationResultCacheMax / delegationResultTTL. Lazily created.
	results map[string]delegationResultEntry
	// nowFn is the clock used for cache TTL/eviction; overridable in tests.
	nowFn func() time.Time
}

// now returns the runner's current time (test-overridable via nowFn).
func (r *agentDelegationRunner) now() time.Time {
	if r.nowFn != nil {
		return r.nowFn()
	}
	return time.Now()
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
		results:  make(map[string]delegationResultEntry),
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

	// Apply the requested per-session persona/model override for this delegated
	// run only. This threads through the same request-scoped SessionLLMOverrides
	// mechanism the ACP server uses, so the target's other concurrent sessions are
	// unaffected and global agent state is never mutated. An unsupported model is
	// ignored downstream (createAgentProvider only honors supported models), so no
	// validation is needed here.
	if ov, ok := delegationSessionOverrides(params); ok {
		agent.SetSessionLLMOverrides(sess.ID, ov)
		// Clear the override once the run completes so this kept session does not
		// silently reuse it on a later turn.
		defer agent.SetSessionLLMOverrides(sess.ID, agent.SessionLLMOverrides{})
	}

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

	res := protocol.DelegationRunResult{
		SessionID:  sess.ID,
		Output:     result.Message.Content().String(),
		StopReason: "end_turn",
	}

	// Retain the result so a caller that restarted after this run can recover it
	// via delegation.status (A2). Stored BEFORE the inflight-delete defer runs, so
	// a concurrent status query transitions running → completed with no "unknown"
	// gap in between.
	r.storeResult(params.CorrelationID, res)

	return res, nil
}

// storeResult records a completed delegation in the bounded result cache, evicting
// expired entries and (when at capacity) the oldest entry first. A blank
// correlation id is not cacheable (status keys on it) and is ignored.
func (r *agentDelegationRunner) storeResult(correlationID string, res protocol.DelegationRunResult) {
	if correlationID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.results == nil {
		r.results = make(map[string]delegationResultEntry)
	}

	now := r.now()
	// Drop expired entries.
	for k, e := range r.results {
		if now.Sub(e.completedAt) > delegationResultTTL {
			delete(r.results, k)
		}
	}
	// Enforce the cap by evicting the oldest entry.
	if _, exists := r.results[correlationID]; !exists && len(r.results) >= delegationResultCacheMax {
		var oldestKey string
		var oldestAt time.Time
		for k, e := range r.results {
			if oldestKey == "" || e.completedAt.Before(oldestAt) {
				oldestKey, oldestAt = k, e.completedAt
			}
		}
		if oldestKey != "" {
			delete(r.results, oldestKey)
		}
	}
	r.results[correlationID] = delegationResultEntry{result: res, completedAt: now}
}

// DelegationStatus reports the state of a delegation by correlation id (A2): it is
// running while the delegation.run is in flight, completed (with the cached result)
// for a short window after it finishes, and unknown otherwise (never seen or
// evicted). It is a read-only query and never re-runs the delegation.
func (r *agentDelegationRunner) DelegationStatus(correlationID string) protocol.DelegationStatusResult {
	if correlationID == "" {
		return protocol.DelegationStatusResult{State: protocol.DelegationStateUnknown}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.inflight[correlationID]; ok {
		return protocol.DelegationStatusResult{State: protocol.DelegationStateRunning}
	}
	if e, ok := r.results[correlationID]; ok {
		if r.now().Sub(e.completedAt) <= delegationResultTTL {
			return protocol.DelegationStatusResult{
				State:      protocol.DelegationStateCompleted,
				SessionID:  e.result.SessionID,
				Output:     e.result.Output,
				StopReason: e.result.StopReason,
			}
		}
		delete(r.results, correlationID) // expired
	}
	return protocol.DelegationStatusResult{State: protocol.DelegationStateUnknown}
}

// delegationSessionOverrides builds the per-session LLM override for a delegated
// run from the requested persona/model. It returns ok=false when neither is set,
// so the target keeps its default model and persona. A requested persona is
// honored authoritatively (PersonaScoped) so it overrides the target's global
// active persona for this session only.
func delegationSessionOverrides(params protocol.DelegationRunParams) (agent.SessionLLMOverrides, bool) {
	var ov agent.SessionLLMOverrides
	set := false
	if m := strings.TrimSpace(params.Model); m != "" {
		ov.Model = models.ModelID(m)
		set = true
	}
	if p := strings.TrimSpace(params.Persona); p != "" {
		ov.Persona = p
		ov.PersonaScoped = true
		set = true
	}
	return ov, set
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
