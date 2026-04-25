package runtime

import (
	"sync"
	"time"
)

type ContainerEvent struct {
	SessionID   string      `json:"sessionId"`
	RuntimeType RuntimeType `json:"runtimeType"`
	ContainerID string      `json:"containerId,omitempty"`
	Event       string      `json:"event"`
	Timestamp   time.Time   `json:"timestamp"`
	Details     string      `json:"details,omitempty"`
}

type EventLog struct {
	mu     sync.Mutex
	max    int
	events []ContainerEvent
	start  int
	size   int
}

func NewEventLog(max int) *EventLog {
	if max <= 0 {
		max = 500
	}
	return &EventLog{
		max:    max,
		events: make([]ContainerEvent, max),
	}
}

func (l *EventLog) Add(event ContainerEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	if l.size < l.max {
		l.events[(l.start+l.size)%l.max] = event
		l.size++
		return
	}

	l.events[l.start] = event
	l.start = (l.start + 1) % l.max
}

func (l *EventLog) List(limit int, sessionID string) []ContainerEvent {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.size == 0 {
		return nil
	}

	if limit <= 0 || limit > l.size {
		limit = l.size
	}

	filtered := make([]ContainerEvent, 0, min(limit, l.size))
	for i := l.size - 1; i >= 0; i-- {
		event := l.events[(l.start+i)%l.max]
		if sessionID != "" && event.SessionID != sessionID {
			continue
		}
		filtered = append(filtered, event)
		if len(filtered) == limit {
			break
		}
	}

	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	return filtered
}
