package agui

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/logging"
)

// Detached runs (P3), parked runs (PANDO-US-0017) and reattachment
// (PANDO-US-0018).
//
// Before frontend tools existed, a run lived exactly as long as its HTTP
// request: the browser POSTed, the handler streamed until RUN_FINISHED, done.
// The interrupt/resume handoff broke that assumption — the run must survive the
// response that reported the interrupt, because the tool result arrives on the
// *next* request. The disconnect/reattach stories extend the same idea to the
// plain "the browser went away mid-turn" case: a dropped connection must not
// waste work already paid for, and a client that comes back should see what it
// missed instead of starting over.
//
// So a run's underlying agent event stream is owned by exactly one goroutine,
// pump (below), for the run's whole lifetime — across parks, disconnects,
// reattaches and interrupt/resume segments alike. Everything else (HTTP
// handlers, possibly several of them at once for one thread) is a *subscriber*:
// it registers, replays what it missed from the bounded buffer, forwards live
// events for as long as it stays attached, and unregisters on its way out. The
// pump is the only code that ever touches a run's translator, which is what
// keeps translator.go's stateful, single-writer design safe under concurrent
// attaches. Every path out of a run other than a suspension ends in
// finishRun, which is what keeps a detached run from outliving its usefulness.

const (
	// suspendGrace bounds how long a suspended run may stay parked waiting for
	// a frontend-tool or permission result. It outlives the tool's own timeout
	// so the model still gets to see the timeout as a tool result and wrap up;
	// only then is the run torn down. This lifetime is independent of
	// Config.DisconnectGrace (PANDO-US-0017): a suspension governs how long a
	// human has to answer a prompt, a disconnect governs how long a dropped
	// connection is given to come back, and the two must not be conflated.
	suspendGrace = defaultFrontendToolTimeout + time.Minute

	// defaultReplayBufferSize bounds the number of AG-UI events a run keeps
	// for a reattaching client to replay (PANDO-US-0017/0018). It holds
	// translated protocol events (not raw agent events), so replay is just
	// "resend what was already computed" — no retranslation, no risk of
	// double-emitting a TOOL_CALL_END. When full, the oldest event is
	// dropped and the buffer marks itself lossy rather than growing without
	// bound: a parked run with nobody listening must not leak memory.
	defaultReplayBufferSize = 500

	// subscriberBufSize sizes a subscriber's live-event channel. Regular
	// broadcasts are best-effort (dropped, never blocking the pump) once a
	// subscriber falls this far behind; the bounded replay buffer is the
	// catch-up mechanism for a client that fell behind or reattached.
	subscriberBufSize = 256

	// finalSendTimeout bounds how long the pump waits trying to deliver the
	// run's last frame(s) to a slow subscriber before giving up and closing
	// it anyway. It exists so a stuck client can never keep the pump — and
	// therefore run teardown — from completing.
	finalSendTimeout = 2 * time.Second
)

// eventBuffer is a bounded ring of AG-UI events a run keeps so a (re)attaching
// client can replay what it missed. See defaultReplayBufferSize.
type eventBuffer struct {
	mu    sync.Mutex
	items []Event
	limit int
	lossy bool
}

func newEventBuffer(limit int) *eventBuffer {
	return &eventBuffer{limit: limit}
}

// append records events, dropping the oldest ones once the buffer is full and
// marking it lossy rather than growing past limit.
func (b *eventBuffer) append(events ...Event) {
	if len(events) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ev := range events {
		if b.limit > 0 && len(b.items) >= b.limit {
			b.items = b.items[1:]
			b.lossy = true
		}
		b.items = append(b.items, ev)
	}
}

// snapshot returns a copy of the buffered events and whether the buffer has
// ever dropped one (lossy sticks once true: a client that replays a lossy
// buffer is told so, even if the buffer has since drained below its limit).
func (b *eventBuffer) snapshot() ([]Event, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, len(b.items))
	copy(out, b.items)
	return out, b.lossy
}

// subscriber is one attached HTTP request's view of a run: the primary
// stream, a reattach, or a read-only follower (a second tab) are all exactly
// this. ch is closed by the run exactly once, when the run truly ends
// (success, error or cancel) — never for a non-terminal segment boundary like
// an interrupt, since the run continues.
type subscriber struct {
	ch chan Event
}

// activeRun is one live agent run backing an AG-UI thread. Its underlying
// agent event stream (events, suspend) is read by exactly one goroutine, the
// pump started when the run is created; every other field guarded by mu may
// be touched from any goroutine (HTTP handlers included).
type activeRun struct {
	threadID  string
	sessionID string
	events    <-chan agent.AgentEvent
	cancel    context.CancelFunc
	state     *stateTracker
	// suspend receives the calls that just blocked on the client.
	suspend <-chan suspension
	// cancelSignal wakes the pump for an explicit cancel (PANDO-US-0019) or an
	// expired park (PANDO-US-0017). Buffered 1: a second signal while one is
	// already pending is redundant and dropped rather than blocking the
	// sender. Only the pump ever receives from it.
	cancelSignal chan string
	// buffer and done are set once at construction and never reassigned, so
	// they need no lock to read.
	buffer *eventBuffer
	done   chan struct{}

	mu sync.Mutex
	// translator is owned by the pump: only it calls Translate/Finish/Fail.
	// Other goroutines only ever replace the pointer (setTranslator), never
	// touch what it points to, which is what keeps translator.go's
	// single-writer design safe here.
	translator *translator
	// ended remembers the tool calls already closed for this thread, so a
	// resumption does not re-close them (see translator.inheritEnded).
	ended   map[string]bool
	reaper  *time.Timer
	stopped bool
	// suspended is true while the run is parked waiting for a frontend-tool
	// or permission result, as opposed to merely disconnected. It decides
	// which grace period a last-subscriber-detach re-arms (see
	// Runtime.attachLoop): the suspended-run reaper must not be shortened —
	// or lengthened — into the disconnect grace, and vice versa.
	suspended bool
	// finished is true once the run has broadcast its terminal frame and
	// closed every subscriber. Once true, subscribe refuses new attaches and
	// broadcast/broadcastFinal are no-ops: a run cannot un-finish.
	finished bool
	subs     map[*subscriber]struct{}
}

// newActiveRun builds a run ready for its pump to be started. t is the
// translator for the run's first segment.
func newActiveRun(threadID, sessionID string, events <-chan agent.AgentEvent, cancel context.CancelFunc, state *stateTracker, suspend <-chan suspension, t *translator) *activeRun {
	return &activeRun{
		threadID:     threadID,
		sessionID:    sessionID,
		events:       events,
		cancel:       cancel,
		state:        state,
		suspend:      suspend,
		cancelSignal: make(chan string, 1),
		buffer:       newEventBuffer(defaultReplayBufferSize),
		done:         make(chan struct{}),
		translator:   t,
		subs:         make(map[*subscriber]struct{}),
	}
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

// setSuspended records whether the run is currently parked waiting for a
// frontend-tool/permission result (see the suspended field doc).
func (a *activeRun) setSuspended(v bool) {
	a.mu.Lock()
	a.suspended = v
	a.mu.Unlock()
}

func (a *activeRun) isSuspended() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.suspended
}

// setTranslator installs the translator for a new segment (a resumption after
// an interrupt). It must be called, and the resumption's pending tool results
// delivered, before pending.resolve wakes the blocked tool goroutine — see
// Runtime.beginResumeSegment — so the pump never observes an event produced by
// the new segment while still holding the old, already-closed-out translator.
func (a *activeRun) setTranslator(t *translator) {
	a.mu.Lock()
	a.translator = t
	a.mu.Unlock()
}

// currentTranslator returns the translator the pump should use right now. It
// is read fresh on every loop iteration rather than cached in a local
// variable, so a segment swap installed by setTranslator is always observed.
func (a *activeRun) currentTranslator() *translator {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.translator
}

// park arms the teardown timer for a run nobody is currently attached to,
// for duration d. It is idempotent to call repeatedly (each call resets the
// timer) and a no-op once the run is stopped or finished.
func (a *activeRun) park(d time.Duration, onExpiry func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.finished {
		return
	}
	if a.reaper != nil {
		a.reaper.Stop()
	}
	a.reaper = time.AfterFunc(d, onExpiry)
}

// unpark disarms the teardown timer when a client (re)attaches.
func (a *activeRun) unpark() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reaper != nil {
		a.reaper.Stop()
		a.reaper = nil
	}
}

// stop cancels the run's agent context once and reports whether this call was
// the one that did it. It never touches the translator or subscribers — see
// requestCancel and finalizeRun for the parts of teardown that must run on
// the pump goroutine.
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

// requestCancel asks the pump to end the run with code as its RUN_ERROR code
// (PANDO-US-0019's cancel endpoint, or an expired park). It cancels the run's
// agent context immediately (stop) so the underlying agent work stops being
// paid for, and wakes the pump to do the rest of teardown — broadcasting the
// final frame and removing the run from the store — since only the pump may
// safely touch the translator. Safe to call more than once or concurrently
// with the pump discovering the same end on its own.
func (a *activeRun) requestCancel(code string) {
	a.stop()
	select {
	case a.cancelSignal <- code:
	default:
	}
}

// subscribe registers a new attach (the original stream, a reattach, or a
// read-only follower) and reports false if the run has already finished —
// there is nothing left to attach to.
func (a *activeRun) subscribe() (*subscriber, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return nil, false
	}
	sub := &subscriber{ch: make(chan Event, subscriberBufSize)}
	a.subs[sub] = struct{}{}
	return sub, true
}

// unsubscribe removes an attach. It reports how many subscribers remain and
// whether the run has already finished, so the caller (Runtime.attachLoop)
// knows whether arming a park timer on "I was the last one" is even
// meaningful — a run that finished on its own does not need parking.
func (a *activeRun) unsubscribe(sub *subscriber) (remaining int, finished bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished {
		return 0, true
	}
	delete(a.subs, sub)
	return len(a.subs), false
}

// broadcast appends events to the replay buffer and best-effort fans them out
// to every attached subscriber. It never blocks the pump: a subscriber that
// cannot keep up simply misses events it can catch up on via the buffer (or,
// if the buffer itself has wrapped, is told the replay is lossy). Used for
// every non-terminal segment boundary — ordinary translated events and an
// interrupt alike — since the run continues past both.
func (a *activeRun) broadcast(events []Event) {
	if len(events) == 0 {
		return
	}
	a.buffer.append(events...)
	a.mu.Lock()
	if a.finished {
		a.mu.Unlock()
		return
	}
	subs := make([]*subscriber, 0, len(a.subs))
	for sub := range a.subs {
		subs = append(subs, sub)
	}
	a.mu.Unlock()

	for _, sub := range subs {
		for _, ev := range events {
			select {
			case sub.ch <- ev:
			default:
			}
		}
	}
}

// broadcastFinal delivers the run's last frame(s) and closes every attached
// subscriber's channel, which is what ends their HTTP responses. Idempotent:
// only the first call has any effect, matching the single terminal transition
// a run may ever make (see the finished field doc).
//
// It does NOT close done — a caller (an attach whose response just ended
// because it read the final frame off its subscriber channel) must not be
// able to observe done before finalizeRun has also removed the run from
// runStore; see finalizeRun.
func (a *activeRun) broadcastFinal(events []Event) {
	a.mu.Lock()
	if a.finished {
		a.mu.Unlock()
		return
	}
	a.finished = true
	subs := a.subs
	a.subs = nil
	a.mu.Unlock()

	a.buffer.append(events...)
	for sub := range subs {
		for _, ev := range events {
			select {
			case sub.ch <- ev:
			case <-time.After(finalSendTimeout):
			}
		}
		close(sub.ch)
	}
}

// replaySnapshot returns the buffered events a (re)attaching client should
// replay before going live, and whether the buffer has dropped any.
func (a *activeRun) replaySnapshot() ([]Event, bool) {
	return a.buffer.snapshot()
}

// subscriberCount reports how many attaches are currently registered. Used
// by tests to synchronize with the pump/attach goroutines without a sleep
// loop guessing at timing.
func (a *activeRun) subscriberCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.subs)
}

// runStore maps AG-UI threads to their live run.
type runStore struct {
	mu sync.Mutex
	m  map[string]*activeRun
	// starts serializes the "decide whether to resume, reject or start a
	// run" section of handleRun per thread (PANDO-US-0018's concurrency fix).
	// Entries are created on first contention and removed once uncontended,
	// so this does not grow without bound across the adapter's lifetime.
	starts map[string]*threadGate
}

// threadGate is one thread's serialization point, refcounted so runStore can
// drop it once nobody is waiting on it.
type threadGate struct {
	mu   sync.Mutex
	refs int
}

func newRunStore() *runStore {
	return &runStore{m: make(map[string]*activeRun), starts: make(map[string]*threadGate)}
}

func (s *runStore) get(threadID string) (*activeRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.m[threadID]
	return run, ok
}

// put inserts run for its thread, but only if the thread has no live run
// yet, reporting whether the insert won. Concurrent creators must serialize
// through Runtime.runs.lockThread (the primary guard against the
// PANDO-US-0018 TOCTOU), not through put being last-write-wins: a racing
// second creator must lose cleanly instead of silently replacing the
// winner's run in the store while both keep running.
func (s *runStore) put(run *activeRun) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.m[run.threadID]; exists {
		return false
	}
	s.m[run.threadID] = run
	return true
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

// lockThread serializes the decide-and-register section of handleRun for one
// thread ID, closing the window where two POSTs both see no live run and both
// race into svc.Run (PANDO-US-0018). It does not serialize different threads
// against each other. The returned func releases the lock and must be called
// exactly once.
func (s *runStore) lockThread(threadID string) func() {
	s.mu.Lock()
	g, ok := s.starts[threadID]
	if !ok {
		g = &threadGate{}
		s.starts[threadID] = g
	}
	g.refs++
	s.mu.Unlock()

	g.mu.Lock()
	return func() {
		g.mu.Unlock()
		s.mu.Lock()
		g.refs--
		if g.refs == 0 {
			delete(s.starts, threadID)
		}
		s.mu.Unlock()
	}
}

// finishRun tears a run down: cancels the agent goroutine, drops the pending
// frontend calls and unregisters the thread. It never touches the
// translator or subscribers, so it is safe to call from any goroutine —
// including outside the pump (e.g. handleDeleteThread). The pump's own
// finalizeRun calls this after it has broadcast the run's terminal frame.
func (r *Runtime) finishRun(run *activeRun) {
	if run.stop() {
		logging.Debug("agui: run finished", "thread", run.threadID, "session", run.sessionID)
		// Released exactly once, guarded by stop()'s once-only return value:
		// finishRun can be (and is) called more than once for the same run
		// (e.g. an explicit cancel racing the pump's own natural finish), and
		// only the call that actually stopped it may free its admission slot
		// (PANDO-US-0021).
		r.admission.release()
	}
	r.pending.discard(run.sessionID)
	r.runs.remove(run)
}

// finalizeRun broadcasts a run's terminal frame(s) to every attached
// subscriber (closing them), tears the run down, and only then closes done.
// Called only from the pump goroutine, since it reads/uses the translator
// that produced final.
//
// done closing last (after runStore no longer holds the run) is what lets a
// caller like handleCancelRun wait on it and then truthfully answer "the run
// is gone from runStore": an attach can observe its subscriber channel close
// — and so return from its HTTP handler — the instant broadcastFinal runs,
// which is before finishRun removes the run; done must not fire that early.
func (r *Runtime) finalizeRun(run *activeRun, final []Event) {
	run.broadcastFinal(final)
	r.finishRun(run)
	close(run.done)
}

// pump is the one goroutine that owns a run's underlying agent event stream
// for its whole lifetime — across parks, disconnects, reattaches and
// interrupt/resume segments. It is started once, when the run is created,
// and is the only reader of run.events, run.suspend and run.cancelSignal, and
// the only caller of run.currentTranslator()'s Translate/Finish/Fail. It
// exits when the run truly ends: naturally (the agent's channel closes, or
// reports AgentEventTypeResponse/Error), or on an explicit cancel/expired
// park (PANDO-US-0019/0017's requestCancel).
//
// A frontend-tool/permission suspension is NOT a pump exit: the same
// goroutine keeps running, idle, until a resumption (Runtime.
// beginResumeSegment) swaps in a new translator and the agent produces more
// events under it.
func (r *Runtime) pump(run *activeRun) {
	for {
		select {
		case code, ok := <-run.cancelSignal:
			if !ok {
				return
			}
			t := run.currentTranslator()
			r.finalizeRun(run, t.Fail(cancelMessage(code), code))
			return

		case s, ok := <-run.suspend:
			if !ok {
				continue
			}
			if run.isSuspended() {
				// A second (parallel) tool call suspended while the first is
				// still awaiting its result: this segment already closed with
				// the first call's interrupt, so describing this one too
				// would mean a second RUN_FINISHED with no RUN_STARTED in
				// between. It stays queued in pendingRegistry and is picked
				// up naturally once the client's next request resolves it —
				// resumeCandidates collects every pending call a POST's
				// trailing tool messages resolve, not just one.
				logging.Debug("agui: a second call suspended while already suspended, deferring",
					"thread", run.threadID, "call", s.callID)
				continue
			}
			r.handleSuspend(run, s)

		case ev, ok := <-run.events:
			if !ok {
				t := run.currentTranslator()
				r.finalizeRun(run, t.Finish(OutcomeSuccess, nil))
				return
			}
			t := run.currentTranslator()
			switch ev.Type {
			case agent.AgentEventTypeResponse:
				result := ev.Message.Content().String()
				r.finalizeRun(run, t.Finish(OutcomeSuccess, result))
				return
			case agent.AgentEventTypeError:
				msg := "unknown error"
				if ev.Error != nil {
					msg = ev.Error.Error()
				}
				r.finalizeRun(run, t.Fail(msg, runErrorCode(ev.Error)))
				return
			default:
				run.broadcast(t.Translate(ev))
			}
		}
	}
}

// cancelMessage renders requestCancel's code as the RUN_ERROR message text.
func cancelMessage(code string) string {
	if code == "expired" {
		return "the run was parked past its grace period with nobody reattached"
	}
	return "the run was cancelled"
}

// handleSuspend ends the current segment with an interrupt while leaving the
// agent blocked on the client, and records that the run is now suspended.
//
// The events already queued by the agent are drained and translated first: a
// client that receives RUN_FINISHED before TOOL_CALL_ARGS has no arguments to
// execute with, and one that sees a permission prompt before the tool call
// that raised it has no context to render.
func (r *Runtime) handleSuspend(run *activeRun, s suspension) {
	t := run.currentTranslator()
	var out []Event
	if len(s.events) > 0 {
		// A synthetic call (a permission prompt): the agent stream will never
		// carry it, so only what it already queued can be drained.
		out = append(out, drainQueuedTranslated(run, t)...)
		t.adoptToolCall(s.callID)
		out = append(out, s.events...)
	} else {
		// A streaming provider has already queued this call's events by the
		// time its tool runs, so flushing the queue is enough.
		out = append(out, drainQueuedTranslated(run, t)...)
		out = append(out, t.completeToolCall(s.callID, callName(s), callInput(s))...)
	}
	out = append(out, t.Finish(OutcomeInterrupt, nil)...)

	run.broadcast(out)
	run.rememberEnded(t.endedSnapshot())
	run.setSuspended(true)
	logging.Debug("agui: run suspended waiting for the client",
		"thread", run.threadID, "call", s.callID)
}

// drainQueuedTranslated forwards whatever the agent has already produced,
// without waiting for more, translating it through t. A permission prompt is
// raised from inside a tool's Run, so the tool call that triggered it is
// guaranteed to be in the channel already.
func drainQueuedTranslated(run *activeRun, t *translator) []Event {
	var out []Event
	for {
		select {
		case ev, ok := <-run.events:
			if !ok {
				return out
			}
			out = append(out, t.Translate(ev)...)
		default:
			return out
		}
	}
}

// callName and callInput read the suspending call's description, which is absent
// only for synthetic calls (they carry their own events instead).
func callName(s suspension) string {
	if s.call == nil {
		return ""
	}
	return s.call.name
}

func callInput(s suspension) string {
	if s.call == nil {
		return ""
	}
	return s.call.input
}
