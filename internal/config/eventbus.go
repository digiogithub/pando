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
	// listeners are the synchronous subscribers registered with
	// SubscribeFunc, keyed by an id so they can be removed.
	listeners map[uint64]func(ConfigChangeEvent)
	nextID    uint64
}

// newEventBus creates an initialised EventBus.
func newEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan ConfigChangeEvent]struct{}),
		listeners:   make(map[uint64]func(ConfigChangeEvent)),
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

// SubscribeFunc registers fn to be called synchronously, on the publisher's
// goroutine, for every future ConfigChangeEvent. It returns a function that
// removes the listener.
//
// Use it instead of Subscribe when the subscriber must read the configuration
// as the change left it: the configuration is not guarded by a lock, and the
// goroutine that published the event is the one that just wrote it, so reading
// it from fn is ordered after that write, whereas a channel subscriber reading
// it later on its own goroutine races with the next writer. fn must be quick
// and must not block; hand any slow work to a goroutine together with a copy of
// the values it needs. It must not call back into the bus.
func (b *EventBus) SubscribeFunc(fn func(ConfigChangeEvent)) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	b.listeners[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.listeners, id)
	}
}

// Publish sends event to every registered subscriber.
// Subscribers that cannot receive (full channel) are skipped to avoid blocking.
// Listeners registered with SubscribeFunc run synchronously, after the channel
// subscribers have been offered the event and outside the bus lock.
func (b *EventBus) Publish(event ConfigChangeEvent) {
	b.mu.RLock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Subscriber is not keeping up; skip this event to avoid blocking.
		}
	}
	listeners := make([]func(ConfigChangeEvent), 0, len(b.listeners))
	for _, fn := range b.listeners {
		listeners = append(listeners, fn)
	}
	b.mu.RUnlock()
	for _, fn := range listeners {
		fn(event)
	}
}

// Bus is the global singleton event bus for configuration changes.
var Bus = newEventBus()
