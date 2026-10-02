package project

import (
	"context"
	"sync"
	"time"
)

// delegationSlots tracks in-flight/queued delegations for one warm target.
// It is shared by ACP child instances and manager-owned web children so both
// routing paths enforce the same per-target cap and queue semantics.
type delegationSlots struct {
	mu sync.Mutex

	inflight     int
	cond         *sync.Cond
	waiters      int
	lastActiveAt time.Time
	closing      bool
}

func newDelegationSlotsAt(ts time.Time) delegationSlots {
	if ts.IsZero() {
		ts = time.Now()
	}
	return delegationSlots{lastActiveAt: ts}
}

func (s *delegationSlots) acquire(max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	if max > 0 && s.inflight >= max {
		return false
	}
	s.inflight++
	s.lastActiveAt = time.Now()
	return true
}

func (s *delegationSlots) acquireOrQueue(ctx context.Context, max, queueDepth int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cond == nil {
		s.cond = sync.NewCond(&s.mu)
	}

	if !s.closing && (max <= 0 || s.inflight < max) {
		s.inflight++
		s.lastActiveAt = time.Now()
		return true
	}

	if s.closing || queueDepth <= 0 || s.waiters >= queueDepth {
		return false
	}

	s.waiters++
	defer func() { s.waiters-- }()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		case <-done:
		}
	}()

	for {
		if ctx.Err() != nil || s.closing {
			return false
		}
		if max <= 0 || s.inflight < max {
			s.inflight++
			s.lastActiveAt = time.Now()
			return true
		}
		s.cond.Wait()
	}
}

func (s *delegationSlots) release() {
	s.mu.Lock()
	if s.inflight > 0 {
		s.inflight--
	}
	s.lastActiveAt = time.Now()
	if s.cond != nil {
		s.cond.Broadcast()
	}
	s.mu.Unlock()
}

func (s *delegationSlots) tryBeginClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.inflight > 0 {
		return false
	}
	s.closing = true
	return true
}

func (s *delegationSlots) beginCloseAndWake() {
	s.mu.Lock()
	s.closing = true
	if s.cond != nil {
		s.cond.Broadcast()
	}
	s.mu.Unlock()
}

func (s *delegationSlots) idleFor(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Sub(s.lastActiveAt)
}

func (s *delegationSlots) inflightCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight
}
