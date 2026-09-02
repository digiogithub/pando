package config

import (
	"sync"
	"time"
)

// ConfigChangeEvent represents a configuration change notification.
type ConfigChangeEvent struct {
	// Section describes which part of the config changed (empty = full reload).
	Section string `json:"section"`
	// Timestamp is when the change occurred.
	Timestamp time.Time `json:"timestamp"`
	// Source identifies the origin of the change: "tui", "webui", "file", or
	// "overlay" for a change imposed by a configuration overlay provider.
	Source string `json:"source"`
	// Event names the kind of change when it is more specific than "something
	// under Section moved". Empty for an ordinary change; see
	// EventOverlayApplied. Subscribers must ignore names they do not know.
	Event string `json:"event,omitempty"`
	// ChangedKeys lists the dotted configuration paths the change touched,
	// when the publisher can name them. Empty means "unknown, assume the
	// whole section changed".
	ChangedKeys []string `json:"changedKeys,omitempty"`
}

// EventBus is a simple fan-out publisher for ConfigChangeEvent.
// Subscribers register a channel; the bus delivers events to all of them.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan ConfigChangeEvent]struct{}
}

// newEventBus creates an initialised EventBus.
func newEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan ConfigChangeEvent]struct{}),
	}
}

// Subscribe registers ch to receive future ConfigChangeEvents.
// The caller owns the channel and is responsible for draining it.
func (b *EventBus) Subscribe(ch chan ConfigChangeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[ch] = struct{}{}
}

// Unsubscribe removes ch from the bus. No more events will be sent to it.
func (b *EventBus) Unsubscribe(ch chan ConfigChangeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, ch)
}

// Publish sends event to every registered subscriber.
// Subscribers that cannot receive (full channel) are skipped to avoid blocking.
func (b *EventBus) Publish(event ConfigChangeEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber is not keeping up; skip this event to avoid blocking.
		}
	}
}

// Bus is the global singleton event bus for configuration changes.
var Bus = newEventBus()
