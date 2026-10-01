package agent

import (
	"context"
	"sort"
	"sync"
)

// ResumeStart starts the system-initiated run of a resumed session and returns
// its event channel (it is a closure over agent.ResumeRun). The context bounds
// the run: cancelling it cancels the run. A handler that takes a run MUST call it
// at most once and read the returned channel until it is closed.
type ResumeStart func(ctx context.Context) (<-chan AgentEvent, error)

// ResumeHandler is offered every run the delegation supervisor resumes for an
// idle session (Case B). It decides whether its surface owns the session:
//
//   - taken == false: not mine, start was NOT called; the next handler is tried.
//   - taken == true: the handler owns the run. It has called start (and is
//     responsible for consuming the events) or failed trying, in which case err is
//     returned. ErrSessionBusy makes the supervisor fall back to live injection.
type ResumeHandler func(sessionID string, start ResumeStart) (taken bool, err error)

// Handler priorities. Handlers are consulted from the highest priority to the
// lowest; handlers of equal priority in registration order. A surface that only
// owns the sessions it opened itself (e.g. an ACP client session) registers at
// ResumePriorityOwner and returns taken == false for any other session; a surface
// that can show any session (the WebUI API) registers at ResumePriorityFallback
// and takes everything that no specific owner claimed.
const (
	ResumePriorityFallback = 0
	ResumePriorityOwner    = 100
)

type resumeEntry struct {
	id       int
	priority int
	handler  ResumeHandler
}

// ResumeRegistry holds the surfaces that may own a resumed run. The zero value
// is not usable; build it with NewResumeRegistry. A nil *ResumeRegistry is safe:
// Offer reports nothing taken, so callers without any surface (TUI, CLI) need no
// special case and fall back to Resume (drain only).
type ResumeRegistry struct {
	mu      sync.Mutex
	nextID  int
	entries []resumeEntry
}

// NewResumeRegistry creates an empty registry.
func NewResumeRegistry() *ResumeRegistry {
	return &ResumeRegistry{}
}

// Register adds a handler and returns a function that removes it again (safe to
// call more than once). A surface registers when it starts serving and
// unregisters when it stops.
func (r *ResumeRegistry) Register(priority int, h ResumeHandler) (unregister func()) {
	if r == nil || h == nil {
		return func() {}
	}
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.entries = append(r.entries, resumeEntry{id: id, priority: priority, handler: h})
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, e := range r.entries {
			if e.id == id {
				r.entries = append(r.entries[:i], r.entries[i+1:]...)
				return
			}
		}
	}
}

// Offer presents a resumed run to the registered handlers, highest priority
// first. It returns taken == true as soon as one handler takes it (err is that
// handler's result); taken == false means nobody owns the session and the caller
// must run it itself (Resume).
func (r *ResumeRegistry) Offer(sessionID string, start ResumeStart) (taken bool, err error) {
	if r == nil {
		return false, nil
	}
	r.mu.Lock()
	entries := make([]resumeEntry, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].priority > entries[j].priority })
	for _, e := range entries {
		if taken, err := e.handler(sessionID, start); taken {
			return true, err
		}
	}
	return false, nil
}
