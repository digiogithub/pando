// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// sessionIndexDebounce is the quiet period after the last qualifying event
// before a session's conversation is (re-)indexed into remembrances. Raised
// from the original 1.2s (see
// [[pando/analysis/session_index_locked_residual_risk.md]] section 2): even
// after filtering out mid-stream deltas (see shouldIndexOnEvent), a single
// turn with several tool calls still finishes multiple assistant/tool
// messages in quick succession, and a short debounce would still fire once
// per leg. 5s coalesces a whole turn into far fewer runs while still
// indexing shortly after the conversation goes quiet.
const sessionIndexDebounce = 5 * time.Second

// sessionIndexMinInterval caps how often a single session may actually run
// through the indexer, regardless of how many qualifying events arrive: no
// two runs for the same session start less than this apart. A trailing run
// is always still scheduled (see sessionIndexScheduler.run) so the latest
// conversation state is never permanently lost — only delayed until the rate
// limit allows it.
const sessionIndexMinInterval = 15 * time.Second

// sessionIndexRunState tracks the debounce/rate-limit/concurrency state for
// one session's index runs. All access must go through sessionIndexScheduler.mu.
type sessionIndexRunState struct {
	timer     *time.Timer
	running   bool
	dirty     bool
	lastRunAt time.Time
}

// sessionIndexScheduler coalesces per-session "please index" notifications
// into a bounded rate of actual runs of indexFn:
//   - a short debounce (debounce) absorbs a burst of qualifying events within
//     one conversation turn into a single run;
//   - minInterval caps the run rate per session even under sustained traffic;
//   - at most one run per session executes at a time;
//   - a notification that arrives while a run is in flight (or while the rate
//     limit defers the next run) is never dropped: it is remembered as
//     "dirty" and guarantees exactly one trailing run once the current run
//     completes and the minimum interval allows it.
//
// A single mutex guards all state; every check-then-act sequence (is a run
// already going? did the session just become dirty?) happens atomically
// under it, so notify() and the timer-fired run() never race each other.
type sessionIndexScheduler struct {
	indexFn     func(ctx context.Context, sessionID string) error
	debounce    time.Duration
	minInterval time.Duration

	mu     sync.Mutex
	states map[string]*sessionIndexRunState
}

// newSessionIndexScheduler creates a scheduler that calls indexFn for a
// session once its notifications have settled, subject to debounce and
// minInterval as described on sessionIndexScheduler.
func newSessionIndexScheduler(indexFn func(ctx context.Context, sessionID string) error, debounce, minInterval time.Duration) *sessionIndexScheduler {
	return &sessionIndexScheduler{
		indexFn:     indexFn,
		debounce:    debounce,
		minInterval: minInterval,
		states:      make(map[string]*sessionIndexRunState),
	}
}

// notify records a qualifying event for sessionID and (re)arms its debounce
// timer, unless a run for that session is currently executing — in which
// case the event is recorded as dirty and picked up by the trailing run once
// the in-flight run finishes. ctx is the base context for whatever run this
// (or a later coalesced) notification eventually triggers; the caller
// controls its lifetime — e.g. passing the watcher's own cancelable context
// so runs, and their retry backoffs, stop promptly on shutdown.
func (s *sessionIndexScheduler) notify(ctx context.Context, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.states[sessionID]
	if st == nil {
		st = &sessionIndexRunState{}
		s.states[sessionID] = st
	}
	if st.running {
		st.dirty = true
		return
	}
	s.scheduleLocked(ctx, sessionID, st, s.debounce)
}

// scheduleLocked (re)arms st.timer to fire after at least baseDelay, extended
// as needed so it never fires sooner than minInterval after the session's
// last completed run. Callers must hold s.mu.
func (s *sessionIndexScheduler) scheduleLocked(ctx context.Context, sessionID string, st *sessionIndexRunState, baseDelay time.Duration) {
	if st.timer != nil {
		st.timer.Stop()
	}
	delay := baseDelay
	if !st.lastRunAt.IsZero() {
		if remaining := s.minInterval - time.Since(st.lastRunAt); remaining > delay {
			delay = remaining
		}
	}
	st.timer = time.AfterFunc(delay, func() {
		s.run(ctx, sessionID, st)
	})
}

// run executes indexFn for sessionID, then schedules exactly one trailing run
// (subject only to the minInterval floor, not a fresh debounce) if the
// session was marked dirty while this run was executing.
func (s *sessionIndexScheduler) run(ctx context.Context, sessionID string, st *sessionIndexRunState) {
	s.mu.Lock()
	if st.running {
		// Defensive: notify()/scheduleLocked() should make this unreachable
		// (a session with a run in flight never gets a new timer), but never
		// let two runs for the same session overlap.
		st.dirty = true
		s.mu.Unlock()
		return
	}
	st.running = true
	st.dirty = false
	s.mu.Unlock()

	if err := s.indexFn(ctx, sessionID); err != nil && !errors.Is(err, context.Canceled) {
		logging.Error("remembrances session index failed", "session_id", sessionID, "error", err)
	}

	s.mu.Lock()
	st.running = false
	st.lastRunAt = time.Now()
	rerun := st.dirty
	if rerun {
		s.scheduleLocked(ctx, sessionID, st, 0)
	}
	s.mu.Unlock()
}

// stopAll stops every pending (not yet fired) timer. Runs already executing
// are not waited on here — matching this watcher's pre-existing shutdown
// behavior of not blocking on in-flight work, and left to the retry loop's
// own ctx-cancellation handling (see replaceSessionEventsWithRetry) to unwind
// promptly.
func (s *sessionIndexScheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.states {
		if st.timer != nil {
			st.timer.Stop()
		}
	}
}
