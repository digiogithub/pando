package agent

import (
	"context"
	"time"
)

// runDrainGrace bounds how long CollectRunResult keeps draining a Run channel
// after it cancels the run because ctx expired. The agent's own cancellation
// path (context.Canceled -> Update(context.Background())) needs a moment to
// finish its in-flight write before the channel closes; this is a generous
// upper bound on that, not a normal-case wait. A var (not a const) so tests
// can shrink it instead of taking seconds to exercise the give-up path.
var runDrainGrace = 5 * time.Second

// CollectRunResult drains a Service.Run channel until it closes and returns the
// terminal event.
//
// The agent guarantees exactly one event is sent immediately before the
// channel is closed — a Response with Done:true on success, or an Error on
// failure (see agent.runInternal) — so simply remembering the last event
// received and returning it once the channel closes is correct: every
// intermediate event (deltas, tool calls, non-terminal compaction errors,
// system messages, token usage, ...) is naturally overwritten before the loop
// ends, leaving only the true terminal event.
//
// Reading a single event and treating it as the result (the bug this helper
// replaces) picks up whichever event happens to arrive first — almost always
// not the terminal one — and then tears down the run's context, interrupting
// whatever write the agent goroutine is in the middle of.
//
// On ctx expiry it calls cancelRun (when non-nil) so the agent goroutine
// unwinds through its own cancellation path, then keeps draining for a
// bounded grace period so any in-flight write finishes before the caller
// tears down resources (contexts, sessions) out from under it. timedOut is
// true whenever ctx expired, regardless of whether the channel then closed
// within the grace period or the grace period itself ran out first.
func CollectRunResult(ctx context.Context, ch <-chan AgentEvent, cancelRun func()) (last AgentEvent, timedOut bool) {
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return last, false
			}
			last = ev
		case <-ctx.Done():
			if cancelRun != nil {
				cancelRun()
			}
			return drainGrace(ch, last)
		}
	}
}

// drainGrace keeps consuming ch for up to runDrainGrace after the caller's
// context expired, so a cancelled run can still finish its current write
// before whatever happens next (cancel(), session deletion, ...).
func drainGrace(ch <-chan AgentEvent, last AgentEvent) (AgentEvent, bool) {
	grace := time.NewTimer(runDrainGrace)
	defer grace.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return last, true
			}
			last = ev
		case <-grace.C:
			// The agent is stuck past its own cancellation; give up rather than
			// leak the caller. Documented as a defect if it is ever observed.
			return last, true
		}
	}
}
