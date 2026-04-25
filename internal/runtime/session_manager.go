package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*SessionEntry
	events   *EventLog
}

type SessionEntry struct {
	SessionID   string           `json:"sessionId"`
	RuntimeType RuntimeType      `json:"runtime"`
	Runtime     ExecutionRuntime `json:"-"`
	ContainerID string           `json:"containerId,omitempty"`
	WorkDir     string           `json:"workDir"`
	CreatedAt   time.Time        `json:"createdAt"`
}

type sessionContainerIDProvider interface {
	sessionContainerID(sessionID string) string
}

var defaultSessionManager = NewSessionManager()

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionEntry),
		events:   NewEventLog(500),
	}
}

func DefaultSessionManager() *SessionManager {
	return defaultSessionManager
}

func (m *SessionManager) GetOrCreate(ctx context.Context, sessionID string, workDir string, runtime ExecutionRuntime) (*SessionEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.sessions[sessionID]; ok {
		if existing.Runtime.Type() == runtime.Type() && existing.WorkDir == workDir {
			return existing, nil
		}
		if err := existing.Runtime.StopSession(ctx, sessionID); err != nil {
			m.RecordEvent(ContainerEvent{
				SessionID:   sessionID,
				RuntimeType: existing.RuntimeType,
				ContainerID: existing.ContainerID,
				Event:       "error",
				Details:     err.Error(),
			})
			return nil, err
		}
		m.RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: existing.RuntimeType,
			ContainerID: existing.ContainerID,
			Event:       "stopped",
			Details:     "session runtime changed",
		})
		delete(m.sessions, sessionID)
	}

	if err := runtime.StartSession(ctx, sessionID, workDir); err != nil {
		m.RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: runtime.Type(),
			Event:       "error",
			Details:     err.Error(),
		})
		return nil, err
	}

	entry := &SessionEntry{
		SessionID:   sessionID,
		RuntimeType: runtime.Type(),
		Runtime:     runtime,
		ContainerID: sessionContainerID(runtime, sessionID),
		WorkDir:     workDir,
		CreatedAt:   time.Now(),
	}
	m.sessions[sessionID] = entry
	m.RecordEvent(ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: entry.RuntimeType,
		ContainerID: entry.ContainerID,
		Event:       "started",
		Details:     workDir,
	})
	return entry, nil
}

func (m *SessionManager) Get(sessionID string) (*SessionEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.sessions[sessionID]
	return entry, ok
}

func (m *SessionManager) Stop(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	entry, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		return nil
	}
	if err := entry.Runtime.StopSession(ctx, sessionID); err != nil {
		m.RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: entry.RuntimeType,
			ContainerID: entry.ContainerID,
			Event:       "error",
			Details:     err.Error(),
		})
		return err
	}
	m.RecordEvent(ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: entry.RuntimeType,
		ContainerID: entry.ContainerID,
		Event:       "stopped",
	})
	return nil
}

func (m *SessionManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	snapshot := make(map[string]*SessionEntry, len(m.sessions))
	for sessionID, entry := range m.sessions {
		snapshot[sessionID] = entry
	}
	m.sessions = make(map[string]*SessionEntry)
	m.mu.Unlock()

	var errs []error
	for sessionID, entry := range snapshot {
		if err := entry.Runtime.StopSession(ctx, sessionID); err != nil {
			m.RecordEvent(ContainerEvent{
				SessionID:   sessionID,
				RuntimeType: entry.RuntimeType,
				ContainerID: entry.ContainerID,
				Event:       "error",
				Details:     err.Error(),
			})
			errs = append(errs, fmt.Errorf("%s: %w", sessionID, err))
			continue
		}
		m.RecordEvent(ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: entry.RuntimeType,
			ContainerID: entry.ContainerID,
			Event:       "stopped",
			Details:     "shutdown",
		})
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (m *SessionManager) List() []SessionEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := make([]SessionEntry, 0, len(m.sessions))
	for _, entry := range m.sessions {
		entries = append(entries, SessionEntry{
			SessionID:   entry.SessionID,
			RuntimeType: entry.RuntimeType,
			ContainerID: entry.ContainerID,
			WorkDir:     entry.WorkDir,
			CreatedAt:   entry.CreatedAt,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})

	return entries
}

func (m *SessionManager) RecordEvent(event ContainerEvent) {
	if m == nil || m.events == nil {
		return
	}
	m.events.Add(event)
}

func (m *SessionManager) Events(limit int, sessionID string) []ContainerEvent {
	if m == nil || m.events == nil {
		return nil
	}
	return m.events.List(limit, sessionID)
}

func sessionContainerID(runtime ExecutionRuntime, sessionID string) string {
	provider, ok := runtime.(sessionContainerIDProvider)
	if !ok {
		return ""
	}
	return provider.sessionContainerID(sessionID)
}
