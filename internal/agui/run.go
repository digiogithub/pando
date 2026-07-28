package agui

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/logging"
)

// Detached runs (P3).
//
// Before frontend tools existed, a run lived exactly as long as its HTTP
// request: the browser POSTed, the handler streamed until RUN_FINISHED, done.
// The interrupt/resume handoff breaks that assumption — the run must survive the
// response that reported the interrupt, because the tool result arrives on the
// *next* request.
//
// So a run is bound to the adapter's lifetime instead of the request's, and the
// request context is used only to notice that the browser went away. Every path
// out of a run other than a suspension calls finishRun, which is what keeps a
// detached run from outliving its usefulness.

// suspendGrace bounds how long a suspended run may stay parked. It outlives the
// tool's own timeout so the model still gets to see the timeout as a tool result
// and wrap up; only then is the run torn down.
const suspendGrace = defaultFrontendToolTimeout + time.Minute

// activeRun is one live agent run backing an AG-UI thread.
type activeRun struct {
	threadID  string
	sessionID string
	events    <-chan agent.AgentEvent
	cancel    context.CancelFunc
	state     *stateTracker
	// suspend receives the calls that just blocked on the client.
	suspend <-chan suspension

	mu sync.Mutex
	// ended remembers the tool calls already closed for this thread, so a
	// resumption does not re-close them (see translator.inheritEnded).
	ended   map[string]bool
	reaper  *time.Timer
	stopped bool
}

// rememberEnded stores the closed calls of the run segment that just ended.
func (a *activeRun) rememberEnded(ended map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ended = ended
}

// endedCalls returns the calls closed before the current segment.
func (a *activeRun) endedCalls() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ended
}

// park arms the teardown timer for a run that is waiting on the browser.
func (a *activeRun) park(onExpiry func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	if a.reaper != nil {
		a.reaper.Stop()
	}
	a.reaper = time.AfterFunc(suspendGrace, onExpiry)
}

// unpark disarms the teardown timer when the client comes back.
func (a *activeRun) unpark() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reaper != nil {
		a.reaper.Stop()
		a.reaper = nil
	}
}

// stop cancels the run once and reports whether this call was the one that did.
func (a *activeRun) stop() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return false
	}
	a.stopped = true
	if a.reaper != nil {
		a.reaper.Stop()
		a.reaper = nil
	}
	a.cancel()
	return true
}

// runStore maps AG-UI threads to their live run.
type runStore struct {
	mu sync.Mutex
	m  map[string]*activeRun
}

func newRunStore() *runStore {
	return &runStore{m: make(map[string]*activeRun)}
}

func (s *runStore) get(threadID string) (*activeRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.m[threadID]
	return run, ok
}

func (s *runStore) put(run *activeRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[run.threadID] = run
}

// remove drops the entry only if it still points at this run, so a run that was
// already replaced by a newer one cannot evict its successor.
func (s *runStore) remove(run *activeRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.m[run.threadID]; ok && current == run {
		delete(s.m, run.threadID)
	}
}

func (s *runStore) all() []*activeRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*activeRun, 0, len(s.m))
	for _, run := range s.m {
		out = append(out, run)
	}
	return out
}

// finishRun tears a run down: cancels the agent goroutine, drops the pending
// frontend calls and unregisters the thread.
func (r *Runtime) finishRun(run *activeRun) {
	if run.stop() {
		logging.Debug("agui: run finished", "thread", run.threadID, "session", run.sessionID)
	}
	r.pending.discard(run.sessionID)
	r.runs.remove(run)
}

// abandonRun ends a run the client walked away from, then waits briefly for the
// agent goroutine to release the session. Without the wait, the immediately
// following Run would race the old goroutine and lose to ErrSessionBusy.
func (r *Runtime) abandonRun(run *activeRun) {
	logging.Info("agui: abandoning previous run for thread", "thread", run.threadID, "session", run.sessionID)
	events := run.events
	r.finishRun(run)

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline.C:
			logging.Warn("agui: previous run did not unwind in time", "thread", run.threadID)
			return
		}
	}
}
