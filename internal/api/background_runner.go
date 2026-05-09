package api

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
)

const (
	// bgEventBufSize is the maximum number of events kept in the replay buffer
	// for each background session. When the buffer is full, the oldest event is
	// dropped to make room for the newest one.
	bgEventBufSize = 500

	// bgSessionTTL is how long a completed session's replay buffer is kept in
	// memory so that late-connecting clients can still observe the final state.
	bgSessionTTL = 10 * time.Minute
)

// bgSession holds the in-flight state of a single background agent run.
type bgSession struct {
	mu     sync.Mutex
	buf    []agent.AgentEvent     // circular replay buffer
	subs   []chan agent.AgentEvent // active SSE subscribers
	done   bool
	cancel context.CancelFunc
	doneAt time.Time
}

// BackgroundSessionManager runs agent sessions in background goroutines that
// are independent of HTTP connections. Clients can connect and disconnect at
// any time; the session keeps running and new subscribers receive a replay of
// the buffered events followed by live updates.
type BackgroundSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*bgSession
}

// NewBackgroundSessionManager creates a manager and starts the background
// cleanup goroutine that purges stale completed sessions.
func NewBackgroundSessionManager() *BackgroundSessionManager {
	m := &BackgroundSessionManager{
		sessions: make(map[string]*bgSession),
	}
	go m.gcLoop()
	return m
}

// IsRunning reports whether the session has an active (or recently completed)
// background run whose replay buffer is still available.
func (m *BackgroundSessionManager) IsRunning(sessionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sessions[sessionID]
	return ok
}

// IsBusy reports whether the session is actively processing (not yet done).
func (m *BackgroundSessionManager) IsBusy(sessionID string) bool {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.done
}

// Submit starts an agent run for sessionID in the background.
// agentRunFn is called with a background-derived context and must return the
// event channel produced by agent.Service.Run().
// Returns agent.ErrSessionBusy if the session is already actively running.
func (m *BackgroundSessionManager) Submit(
	sessionID string,
	agentRunFn func(ctx context.Context) (<-chan agent.AgentEvent, error),
) error {
	m.mu.Lock()
	if s, exists := m.sessions[sessionID]; exists && !s.done {
		m.mu.Unlock()
		return agent.ErrSessionBusy
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &bgSession{
		buf:    make([]agent.AgentEvent, 0, bgEventBufSize),
		cancel: cancel,
	}
	m.sessions[sessionID] = s
	m.mu.Unlock()

	eventCh, err := agentRunFn(ctx)
	if err != nil {
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		cancel()
		return err
	}

	go m.pump(sessionID, s, eventCh)
	return nil
}

// pump reads events from the agent channel, appends them to the replay buffer,
// and fans them out to all current subscribers.
func (m *BackgroundSessionManager) pump(sessionID string, s *bgSession, eventCh <-chan agent.AgentEvent) {
	for event := range eventCh {
		s.mu.Lock()
		// Ring-buffer semantics: drop oldest when full.
		if len(s.buf) >= bgEventBufSize {
			s.buf = s.buf[1:]
		}
		s.buf = append(s.buf, event)
		for _, sub := range s.subs {
			select {
			case sub <- event:
			default:
				// Slow subscriber — skip rather than block the agent.
			}
		}
		s.mu.Unlock()
	}

	// Agent channel closed → run finished.
	s.mu.Lock()
	s.done = true
	s.doneAt = time.Now()
	for _, sub := range s.subs {
		close(sub)
	}
	s.subs = nil
	s.mu.Unlock()
}

// Subscribe returns:
//   - events: a channel that first delivers buffered events then live events.
//   - unsubscribe: call this when done to remove the subscriber (safe to call
//     multiple times; if the session is already done it is a no-op).
//   - ok: false when no session with that ID exists in the manager.
//
// If the session is already done the channel is closed after the replay.
func (m *BackgroundSessionManager) Subscribe(sessionID string) (events <-chan agent.AgentEvent, unsubscribe func(), ok bool) {
	m.mu.RLock()
	s, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if !exists {
		return nil, func() {}, false
	}

	s.mu.Lock()
	// Pre-allocate enough capacity for the replay burst plus future live events.
	sub := make(chan agent.AgentEvent, len(s.buf)+256)

	// Replay buffered events before registering as a live subscriber so the
	// caller receives history in order without a race condition.
	for _, ev := range s.buf {
		sub <- ev
	}

	if s.done {
		close(sub)
	} else {
		s.subs = append(s.subs, sub)
	}
	s.mu.Unlock()

	unsubFn := func() {
		m.mu.RLock()
		session, still := m.sessions[sessionID]
		m.mu.RUnlock()
		if !still {
			return
		}
		session.mu.Lock()
		defer session.mu.Unlock()
		for i, ch := range session.subs {
			if ch == sub {
				session.subs = append(session.subs[:i], session.subs[i+1:]...)
				return
			}
		}
	}

	return sub, unsubFn, true
}

// Cancel cancels the background run for sessionID. No-op if unknown.
func (m *BackgroundSessionManager) Cancel(sessionID string) {
	m.mu.RLock()
	s, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if ok {
		s.cancel()
	}
}

// gcLoop periodically removes completed sessions whose TTL has expired.
func (m *BackgroundSessionManager) gcLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.gc()
	}
}

func (m *BackgroundSessionManager) gc() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.mu.Lock()
		expired := s.done && now.Sub(s.doneAt) > bgSessionTTL
		s.mu.Unlock()
		if expired {
			delete(m.sessions, id)
		}
	}
}
