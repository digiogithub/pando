package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/conclusion"
	mesnadaOrch "github.com/digiogithub/pando/internal/mesnada/orchestrator"
	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// delegationAgent is the narrow slice of agent.Service the supervisor needs to
// decide Case A injection (live loop) and Case B resurrection (idle loop).
// Factoring it out keeps handleCompletion/flush unit-testable without stubbing
// the whole agent.Service interface. *agent satisfies it.
type delegationAgent interface {
	IsSessionBusy(sessionID string) bool
	InjectConclusion(sessionID string, content string) error
	Resume(ctx context.Context, sessionID string, content string) error
	ResurrectionCount(sessionID string) int
}

// parentLister is the slice of the orchestrator the supervisor needs to discover
// whether a parent session still has outstanding sibling tasks. *Orchestrator
// satisfies it; tests stub it.
type parentLister interface {
	ListByParentSession(sessionID string) ([]*models.Task, error)
}

// awaitReader is the slice of the orchestrator the supervisor needs to consult and
// clear a session's non-blocking await intent (registered by the mesnada_await
// tool). *Orchestrator satisfies it; tests stub it. It is optional: when nil the
// supervisor falls back to the Phase-3 (no-await) resurrection behavior.
type awaitReader interface {
	AwaitIntentFor(sessionID string) (*mesnadaOrch.AwaitIntent, bool)
	ClearAwaitIntent(sessionID string)
}

// delegationSupervisorOptions carries the runtime knobs the supervisor reads from
// config. Default-off: when Enabled is false, or neither InjectIntoLiveLoop nor
// ResurrectIdleLoop is set, the supervisor is never started, so there is no
// overhead and behaviour is unchanged.
type delegationSupervisorOptions struct {
	Enabled             bool
	InjectIntoLiveLoop  bool
	ResurrectIdleLoop   bool
	MaxResurrections    int           // per session per turn-chain (auto-resets on user Run)
	MaxDepth            int           // delegated-of-delegated cap (0 = unlimited)
	ResurrectionTimeout time.Duration // how long to batch sibling completions before flushing
}

// sessionBatch accumulates completed sibling tasks for a parent session that are
// not yet flushed, plus the debounce timer that will flush them when no more
// siblings are outstanding (or the timeout fires).
type sessionBatch struct {
	tasks []*models.Task
	timer *time.Timer
	// awaitTimer is the single safety timer armed at an await intent's Deadline when
	// the join condition is not yet satisfied. It is distinct from timer (the
	// Phase-3 debounce timer) so the two batching modes never interfere.
	awaitTimer *time.Timer
}

// delegationSupervisor implements both re-entry cases of the delegation protocol:
//   - Case A (InjectIntoLiveLoop): when a delegated task completes while its parent
//     agent session is still running, inject the conclusion into that live loop via
//     the steering inbox.
//   - Case B (ResurrectIdleLoop): when a delegated task completes while the parent
//     session is idle, batch near-simultaneous sibling completions and resurrect
//     the parent loop once with the combined results.
type delegationSupervisor struct {
	// orchConcrete is the concrete orchestrator used for the completion
	// subscription (Start). orch is the parentLister slice used for the
	// outstanding-sibling query (tests stub orch directly).
	orchConcrete *mesnadaOrch.Orchestrator
	orch         parentLister
	awaits       awaitReader // optional; nil => no await-aware behavior
	agent        delegationAgent
	opts         delegationSupervisorOptions
	ctx          context.Context

	mu      sync.Mutex
	seen    map[string]struct{}      // dedupe keys (CorrelationID, or task.ID fallback) already handled
	batches map[string]*sessionBatch // pending resurrection batches keyed by parent session id

	unsubscribe func()
	wg          sync.WaitGroup
}

// newDelegationSupervisor builds a supervisor. It does not subscribe or start any
// goroutine until Start is called. orchSub is the concrete orchestrator used for
// the completion subscription; the supervisor only needs its parentLister slice
// for outstanding-sibling checks.
func newDelegationSupervisor(orch *mesnadaOrch.Orchestrator, ag delegationAgent, opts delegationSupervisorOptions) *delegationSupervisor {
	s := &delegationSupervisor{
		agent:   ag,
		opts:    opts,
		seen:    make(map[string]struct{}),
		batches: make(map[string]*sessionBatch),
	}
	if orch != nil {
		s.orchConcrete = orch
		s.orch = orch
		s.awaits = orch
	}
	return s
}

// Start subscribes to orchestrator completions and runs the dispatch loop tied to
// ctx. It is a no-op when delegation is disabled, when neither re-entry mode is
// on, or when its dependencies are missing — so no subscription is created and
// there is zero runtime overhead in the default-off configuration.
func (s *delegationSupervisor) Start(ctx context.Context) {
	if s == nil || s.orchConcrete == nil || s.agent == nil {
		return
	}
	if !s.opts.Enabled || (!s.opts.InjectIntoLiveLoop && !s.opts.ResurrectIdleLoop) {
		return
	}
	s.ctx = ctx

	completions, unsubscribe := s.orchConcrete.SubscribeCompletions()
	s.unsubscribe = unsubscribe

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case task, ok := <-completions:
				if !ok {
					return
				}
				s.handleCompletion(task)
			}
		}
	}()
	logging.Debug("Delegation supervisor started",
		"liveInjection", s.opts.InjectIntoLiveLoop, "resurrectIdle", s.opts.ResurrectIdleLoop)
}

// Stop unsubscribes from the orchestrator, stops any pending batch timers, and
// waits for the dispatch loop to exit. It is safe to call when Start was never
// invoked. It does not flush pending batches — pending resurrections are dropped
// cleanly on shutdown.
func (s *delegationSupervisor) Stop() {
	if s == nil {
		return
	}
	if s.unsubscribe != nil {
		s.unsubscribe()
	}
	s.mu.Lock()
	for id, b := range s.batches {
		if b != nil {
			if b.timer != nil {
				b.timer.Stop()
			}
			if b.awaitTimer != nil {
				b.awaitTimer.Stop()
			}
		}
		delete(s.batches, id)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// handleCompletion applies the re-entry decision for a single completed task. It
// runs in the single dispatch goroutine. Returns true when an action (live
// injection or batch enqueue/flush) was performed (for tests).
func (s *delegationSupervisor) handleCompletion(task *models.Task) bool {
	if task == nil {
		return false
	}
	if !s.opts.Enabled {
		return false
	}
	// Only correlated tasks with a captured conclusion are eligible.
	if task.ParentSessionID == "" || task.Conclusion == nil {
		return false
	}

	// Idempotency: dedupe by CorrelationID (fall back to task.ID when empty) BEFORE
	// any injection or batching so the same task is never handled twice.
	key := dedupeKey(task)
	s.mu.Lock()
	if _, done := s.seen[key]; done {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	if s.agent.IsSessionBusy(task.ParentSessionID) {
		return s.injectLive(task, key)
	}
	return s.enqueueResurrection(task, key)
}

// injectLive performs Case A: inject into a still-running parent loop.
func (s *delegationSupervisor) injectLive(task *models.Task, key string) bool {
	if !s.opts.InjectIntoLiveLoop {
		logging.Debug("Delegation: live injection disabled, skipping",
			"task", task.ID, "parentSession", task.ParentSessionID)
		return false
	}
	content := conclusion.FormatForParent(task)
	if err := s.agent.InjectConclusion(task.ParentSessionID, content); err != nil {
		if errors.Is(err, agent.ErrSessionNotBusy) {
			// Race: the session ended between the busy check and injection. Leave the
			// task unmarked so a later completion event (or resurrection) can handle it.
			logging.Debug("Delegation: parent session ended before injection",
				"task", task.ID, "parentSession", task.ParentSessionID)
			return false
		}
		logging.Warn("Delegation: failed to inject conclusion",
			"task", task.ID, "parentSession", task.ParentSessionID, "error", err)
		return false
	}
	// Mark only on success so a transient failure can be retried by a later event.
	s.mu.Lock()
	s.seen[key] = struct{}{}
	s.mu.Unlock()
	logging.Debug("Delegation: injected conclusion into live parent loop",
		"task", task.ID, "parentSession", task.ParentSessionID)
	return true
}

// enqueueResurrection performs Case B bookkeeping: it caps depth/budget, marks the
// task seen, appends it to the parent's pending batch, then either flushes
// immediately (no outstanding siblings) or arms the debounce timer.
func (s *delegationSupervisor) enqueueResurrection(task *models.Task, key string) bool {
	if !s.opts.ResurrectIdleLoop {
		logging.Debug("Delegation: parent session idle and resurrection disabled, skipping",
			"task", task.ID, "parentSession", task.ParentSessionID)
		return false
	}
	parentSession := task.ParentSessionID

	// Depth cap: prevent deep delegated-of-delegated chains.
	if s.opts.MaxDepth > 0 && task.Depth >= s.opts.MaxDepth {
		logging.Debug("Delegation: depth cap reached, skipping resurrection",
			"task", task.ID, "depth", task.Depth, "maxDepth", s.opts.MaxDepth)
		return false
	}
	// Resurrection cap: enforce the per-turn-chain budget (auto-resets on user Run).
	if s.opts.MaxResurrections > 0 && s.agent.ResurrectionCount(parentSession) >= s.opts.MaxResurrections {
		logging.Debug("Delegation: resurrection budget exhausted, skipping",
			"task", task.ID, "parentSession", parentSession, "max", s.opts.MaxResurrections)
		return false
	}

	// Mark seen now (we have decided to act on this task) and append to the batch.
	s.mu.Lock()
	s.seen[key] = struct{}{}
	b := s.batches[parentSession]
	if b == nil {
		b = &sessionBatch{}
		s.batches[parentSession] = b
	}
	b.tasks = append(b.tasks, task)
	s.mu.Unlock()

	// Await-aware path: when the parent registered a non-blocking await intent (via
	// the mesnada_await tool) we suppress the default per-completion / debounce
	// flush and instead resurrect ONLY when the intent's join condition is satisfied
	// (or its deadline passes). The completed task's conclusion is already in the
	// batch above, so nothing is lost even for incidental non-awaited completions.
	if intent, ok := s.awaitIntentFor(parentSession); ok {
		return s.handleAwaitCompletion(parentSession, b, intent)
	}

	// No await intent: keep the existing Phase 3 behavior. Decide whether to flush
	// now or wait for outstanding siblings.
	if s.hasOutstandingSiblings(parentSession) {
		s.mu.Lock()
		if b.timer != nil {
			b.timer.Stop()
		}
		b.timer = time.AfterFunc(s.opts.ResurrectionTimeout, func() { s.flush(parentSession) })
		s.mu.Unlock()
		logging.Debug("Delegation: siblings outstanding, batching resurrection",
			"parentSession", parentSession, "timeout", s.opts.ResurrectionTimeout)
		return true
	}
	s.flush(parentSession)
	return true
}

// awaitIntentFor returns the parent session's active await intent, if the
// supervisor has an await reader wired (it always does in production; tests may
// not). It is a thin nil-safe wrapper.
func (s *delegationSupervisor) awaitIntentFor(parentSession string) (*mesnadaOrch.AwaitIntent, bool) {
	if s.awaits == nil {
		return nil, false
	}
	return s.awaits.AwaitIntentFor(parentSession)
}

// handleAwaitCompletion evaluates a registered await intent on each eligible idle
// completion. It resurrects (flushing the accumulated batch) only when the join
// condition is satisfied or the deadline has passed; otherwise it arms a single
// safety timer at the deadline so a never-completing awaited task cannot hang the
// session forever. b is the (already-appended-to) batch for parentSession.
func (s *delegationSupervisor) handleAwaitCompletion(parentSession string, b *sessionBatch, intent *mesnadaOrch.AwaitIntent) bool {
	satisfied := s.awaitJoinSatisfied(intent)
	deadlinePassed := !intent.Deadline.IsZero() && time.Now().After(intent.Deadline)

	if satisfied || deadlinePassed {
		if s.awaits != nil {
			s.awaits.ClearAwaitIntent(parentSession)
		}
		s.mu.Lock()
		if b.awaitTimer != nil {
			b.awaitTimer.Stop()
			b.awaitTimer = nil
		}
		s.mu.Unlock()
		logging.Debug("Delegation: await condition met, resurrecting",
			"parentSession", parentSession, "policy", string(intent.Policy),
			"satisfied", satisfied, "deadlinePassed", deadlinePassed)
		s.flushAwait(parentSession, satisfied)
		return true
	}

	// Not yet satisfied: arm a single safety timer at the deadline (once) so a
	// stuck awaited task still wakes the parent eventually.
	if !intent.Deadline.IsZero() {
		s.mu.Lock()
		if b.awaitTimer == nil {
			d := time.Until(intent.Deadline)
			if d < 0 {
				d = 0
			}
			b.awaitTimer = time.AfterFunc(d, func() {
				if s.awaits != nil {
					s.awaits.ClearAwaitIntent(parentSession)
				}
				s.flushAwait(parentSession, false)
			})
		}
		s.mu.Unlock()
	}
	logging.Debug("Delegation: await condition not yet met, batching",
		"parentSession", parentSession, "policy", string(intent.Policy))
	return true
}

// awaitJoinSatisfied evaluates the intent's join policy against current task
// statuses: a task in ANY terminal state counts as "reported" (failed tasks carry
// a synthesized conclusion). On a list error it conservatively returns false so a
// resurrection is not triggered prematurely (the deadline timer still bounds it).
func (s *delegationSupervisor) awaitJoinSatisfied(intent *mesnadaOrch.AwaitIntent) bool {
	if intent == nil || len(intent.TaskIDs) == 0 {
		return true
	}
	if s.orch == nil {
		return false
	}
	tasks, err := s.orch.ListByParentSession(intent.SessionID)
	if err != nil {
		logging.Warn("Delegation: failed to list tasks for await evaluation",
			"parentSession", intent.SessionID, "error", err)
		return false
	}
	status := make(map[string]bool, len(tasks)) // task id -> terminal?
	for _, t := range tasks {
		if t != nil {
			status[t.ID] = t.IsTerminal()
		}
	}
	reported := 0
	for _, id := range intent.TaskIDs {
		if status[id] {
			reported++
		}
	}
	total := len(intent.TaskIDs)
	switch intent.Policy {
	case mesnadaOrch.AwaitPolicyAny:
		return reported >= 1
	case mesnadaOrch.AwaitPolicyQuorum:
		if intent.Quorum <= 0 {
			return reported >= total
		}
		return reported >= intent.Quorum
	default: // all
		return reported >= total
	}
}

// hasOutstandingSiblings reports whether the parent session still has any
// correlated task in a non-terminal state (pending/running/paused). Paused counts
// as outstanding for resurrection purposes (it may resume). On query error it
// assumes none are outstanding so a resurrection is not stalled forever.
func (s *delegationSupervisor) hasOutstandingSiblings(parentSession string) bool {
	if s.orch == nil {
		return false
	}
	tasks, err := s.orch.ListByParentSession(parentSession)
	if err != nil {
		logging.Warn("Delegation: failed to list sibling tasks, assuming none outstanding",
			"parentSession", parentSession, "error", err)
		return false
	}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		switch t.Status {
		case models.TaskStatusPending, models.TaskStatusRunning, models.TaskStatusPaused:
			return true
		}
	}
	return false
}

// flush takes and clears the pending batch for a session and resurrects the parent
// loop once with the combined conclusions (opportunistic, no-await framing). It is
// safe to call from both the dispatch goroutine (immediate flush) and a timer
// goroutine.
func (s *delegationSupervisor) flush(parentSession string) {
	s.flushWith(parentSession, buildResurrectionContent)
}

// flushAwait flushes the batch for a session whose await intent resolved, framing
// the resurrection as an await wake (satisfied vs timed out).
func (s *delegationSupervisor) flushAwait(parentSession string, satisfied bool) {
	s.flushWith(parentSession, func(tasks []*models.Task) string {
		return buildAwaitResurrectionContent(tasks, satisfied)
	})
}

// flushWith is the shared flush core: it takes and clears the pending batch for a
// session and resurrects the parent loop once with content built by build.
func (s *delegationSupervisor) flushWith(parentSession string, build func([]*models.Task) string) {
	s.mu.Lock()
	b := s.batches[parentSession]
	if b == nil || len(b.tasks) == 0 {
		if b != nil {
			if b.timer != nil {
				b.timer.Stop()
			}
			if b.awaitTimer != nil {
				b.awaitTimer.Stop()
			}
		}
		delete(s.batches, parentSession)
		s.mu.Unlock()
		return
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	if b.awaitTimer != nil {
		b.awaitTimer.Stop()
	}
	tasks := b.tasks
	delete(s.batches, parentSession)
	s.mu.Unlock()

	// Re-check the resurrection cap at flush time (it may have been exhausted by a
	// concurrent flush for the same session, or reset by a user Run).
	if s.opts.MaxResurrections > 0 && s.agent.ResurrectionCount(parentSession) >= s.opts.MaxResurrections {
		logging.Debug("Delegation: resurrection budget exhausted at flush, dropping batch",
			"parentSession", parentSession, "tasks", len(tasks))
		return
	}

	content := build(tasks)
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.agent.Resume(ctx, parentSession, content); err != nil {
		if errors.Is(err, agent.ErrSessionBusy) {
			// Race: the user/agent became active again. Fall back to Case A live
			// injection so the results are not lost.
			if injErr := s.agent.InjectConclusion(parentSession, content); injErr != nil {
				logging.Warn("Delegation: resume race fallback injection failed",
					"parentSession", parentSession, "error", injErr)
				return
			}
			logging.Debug("Delegation: resume race, injected into now-live loop instead",
				"parentSession", parentSession, "tasks", len(tasks))
			return
		}
		logging.Warn("Delegation: failed to resurrect idle parent loop",
			"parentSession", parentSession, "error", err)
		return
	}
	logging.Debug("Delegation: resurrected idle parent loop",
		"parentSession", parentSession, "tasks", len(tasks))
}

// dedupeKey returns the idempotency key for a completed task.
func dedupeKey(task *models.Task) string {
	if task.CorrelationID != "" {
		return task.CorrelationID
	}
	return task.ID
}

// buildResurrectionContent frames the combined conclusions as the opening turn of
// a resurrected run: a short system preamble plus each task's FormatForParent
// rendering, joined by blank lines.
func buildResurrectionContent(tasks []*models.Task) string {
	var b strings.Builder
	b.WriteString("You are resuming because the following delegated task(s) reported their results. ")
	b.WriteString("Continue, spawn more work, or finish.\n")
	for _, t := range tasks {
		b.WriteString("\n")
		b.WriteString(conclusion.FormatForParent(t))
		b.WriteString("\n")
	}
	return b.String()
}

// buildAwaitResurrectionContent frames a resurrection that was triggered by a
// satisfied (or timed-out) await intent the model registered via mesnada_await, so
// the model knows it woke because the wait it requested resolved.
func buildAwaitResurrectionContent(tasks []*models.Task, satisfied bool) string {
	var b strings.Builder
	if satisfied {
		b.WriteString("You are resuming because the wait you registered (mesnada_await) has been satisfied. ")
	} else {
		b.WriteString("You are resuming because the safety timeout for the wait you registered (mesnada_await) elapsed before it was fully satisfied. ")
	}
	b.WriteString("The delegated task results so far:\n")
	for _, t := range tasks {
		b.WriteString("\n")
		b.WriteString(conclusion.FormatForParent(t))
		b.WriteString("\n")
	}
	return b.String()
}
