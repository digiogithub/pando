package events

import "sync"

// EventBus fans a single upstream event source (typically one
// long-lived, backend-specific listener -- a D-Bus signal match, a CDP
// ListenTarget handler, ...) out to any number of independent waiters,
// each with its own buffered channel so one slow consumer never blocks
// another or the publisher.
type EventBus struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[uint64]chan Event
	closed bool
}

// NewEventBus creates an empty EventBus.
func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[uint64]chan Event)}
}

// defaultBuffer is used when Subscribe is called with buffer <= 0.
const defaultBuffer = 16

// Subscribe registers a new waiter with the given channel buffer size
// (<=0 uses a small default), returning its receive-only channel and an
// unsubscribe func. Unsubscribe is idempotent and safe to call from any
// goroutine, any number of times. Subscribing to a closed bus returns an
// already-closed channel and a no-op unsubscribe.
func (b *EventBus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = defaultBuffer
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	id := b.nextID
	b.nextID++
	ch := make(chan Event, buffer)
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			b.mu.Lock()
			if c, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(c)
			}
			b.mu.Unlock()
		})
	}
	return ch, unsub
}

// Publish fans ev out to every current subscriber. A subscriber whose
// buffer is full has the event dropped for it rather than blocking the
// publisher: this is a deliberate best-effort design, not a bug -- see
// Subscriber's doc comment.
func (b *EventBus) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Len reports the current subscriber count.
func (b *EventBus) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Close unsubscribes and closes every current subscriber channel and
// causes any future Subscribe call to return an already-closed channel.
// Safe to call more than once.
func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}
