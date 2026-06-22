package orchestrator

import (
	"time"
)

// AwaitPolicy is the join policy a parent agent registers when it awaits one or
// more delegated tasks before ending its turn.
type AwaitPolicy string

const (
	// AwaitPolicyAll resolves when every awaited task has reported (reached a
	// terminal state).
	AwaitPolicyAll AwaitPolicy = "all"
	// AwaitPolicyAny resolves as soon as at least one awaited task has reported.
	AwaitPolicyAny AwaitPolicy = "any"
	// AwaitPolicyQuorum resolves when at least Quorum awaited tasks have reported.
	AwaitPolicyQuorum AwaitPolicy = "quorum"
)

// AwaitIntent records a parent agent session's intent to wait, non-blocking, for
// a set of correlated delegated tasks. The await tool registers it and ends the
// model's turn; the delegation supervisor consults it on each completion and
// resurrects the idle parent loop once the join condition is satisfied (or the
// Deadline passes). It is in-memory only and not persisted: it mirrors the
// supervisor's own in-memory batch state, so neither survives a restart (the
// task conclusions themselves remain durable on the store and re-fetchable).
type AwaitIntent struct {
	// SessionID is the parent agent session that is awaiting.
	SessionID string
	// TaskIDs are the awaited correlated task ids. The await tool resolves an empty
	// caller-supplied list to all currently-outstanding correlated tasks before
	// registering, so this is never empty once stored.
	TaskIDs []string
	// Policy is the join policy ("all" | "any" | "quorum").
	Policy AwaitPolicy
	// Quorum is the number of reported tasks required when Policy is quorum.
	Quorum int
	// Deadline is a safety timeout after which the supervisor flushes whatever has
	// accumulated even if the join condition is not yet satisfied. Zero means none.
	Deadline time.Time
	// CreatedAt is when the intent was registered.
	CreatedAt time.Time
}

// SetAwaitIntent registers (replacing any existing) the await intent for a
// session. Safe for concurrent use.
func (o *Orchestrator) SetAwaitIntent(intent *AwaitIntent) {
	if intent == nil || intent.SessionID == "" {
		return
	}
	o.awaitMu.Lock()
	defer o.awaitMu.Unlock()
	if o.awaitIntents == nil {
		o.awaitIntents = make(map[string]*AwaitIntent)
	}
	o.awaitIntents[intent.SessionID] = intent
}

// AwaitIntentFor returns the active await intent for a session, if any.
func (o *Orchestrator) AwaitIntentFor(sessionID string) (*AwaitIntent, bool) {
	o.awaitMu.Lock()
	defer o.awaitMu.Unlock()
	intent, ok := o.awaitIntents[sessionID]
	return intent, ok
}

// ClearAwaitIntent removes the await intent for a session (no-op if none).
func (o *Orchestrator) ClearAwaitIntent(sessionID string) {
	o.awaitMu.Lock()
	defer o.awaitMu.Unlock()
	delete(o.awaitIntents, sessionID)
}
