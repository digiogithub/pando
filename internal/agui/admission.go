package agui

import "sync"

// runAdmission enforces Config.MaxConcurrentRuns (PANDO-US-0021): a
// process-wide cap on runs the adapter has admitted, reserved before any
// session, agent instance or thread binding is created for a request, so a
// rejected request leaves no state behind (see Runtime.handleRun).
//
// A slot is held for a run's whole lifetime, including while it is suspended
// waiting on a client -- a parked run still occupies a slot (its session is
// pinned for up to suspendGrace) and releasing it there would let the cap be
// silently oversubscribed by piling up permission prompts nobody is
// answering. It is released exactly once, when the run truly ends (see
// Runtime.finishRun), never on a mere disconnect or suspension.
//
// A resumption of an already-admitted run (Runtime.beginResumeSegment) never
// calls tryAdmit again: it re-attaches to a run that already holds its slot.
type runAdmission struct {
	mu      sync.Mutex
	max     int
	current int
}

// newRunAdmission builds an admission gate for the given cap. max <= 0 means
// unlimited, preserving the adapter's pre-PANDO-US-0021 behaviour.
func newRunAdmission(max int) *runAdmission {
	return &runAdmission{max: max}
}

// tryAdmit reserves a slot, reporting whether it succeeded. A nil receiver
// (a Runtime built directly in a test, without going through New) always
// admits, matching "unlimited" -- tests that want to exercise the cap set
// Runtime.admission explicitly.
func (a *runAdmission) tryAdmit() bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.max > 0 && a.current >= a.max {
		return false
	}
	a.current++
	return true
}

// release frees a slot reserved by a prior successful tryAdmit. Callers must
// release exactly once per successful tryAdmit -- see Runtime.finishRun,
// which guards this with activeRun.stop()'s once-only semantics, and the
// early-failure branches in Runtime.handleRun for the case where a slot was
// reserved but the run was never actually created.
func (a *runAdmission) release() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.current--
	a.mu.Unlock()
}

// snapshot reports the current and maximum run counts, for the /healthz
// payload (PANDO-US-0020/0021) and rejection log lines.
func (a *runAdmission) snapshot() (current, max int) {
	if a == nil {
		return 0, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current, a.max
}
