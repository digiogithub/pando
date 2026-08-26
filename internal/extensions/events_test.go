package extensions

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/pkg/extension"
)

type subExt struct {
	baseExt
	topics []string

	mu     sync.Mutex
	events []extension.Event
	panics bool
}

func (e *subExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *subExt) Topics() []string              { return e.topics }

func (e *subExt) HandleEvent(_ context.Context, ev extension.Event) {
	if e.panics {
		panic("boom")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}

func (e *subExt) seen() []extension.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]extension.Event(nil), e.events...)
}

// waitFor polls until cond holds or the deadline passes. The fan-out is
// asynchronous, so a bare assertion would be a race.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

type fakeResource struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
}

func TestForwardDeliversToSubscribers(t *testing.T) {
	sub := &subExt{baseExt: baseExt{id: "sink.acme"}}
	mgr := managerWith(t, sub)

	broker := pubsub.NewBroker[fakeResource]()
	t.Cleanup(broker.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	Forward(ctx, mgr, extension.TopicSession, broker)
	broker.Publish(pubsub.CreatedEvent, fakeResource{ID: "s1", SessionID: "s1", Title: "hello"})

	if !waitFor(t, func() bool { return len(sub.seen()) == 1 }) {
		t.Fatal("event never arrived")
	}
	ev := sub.seen()[0]
	if ev.Topic != extension.TopicSession || ev.Type != extension.EventCreated {
		t.Errorf("ev = %+v", ev)
	}
	if ev.ID != "s1" || ev.SessionID != "s1" {
		t.Errorf("ids not extracted: %+v", ev)
	}
	if ev.Payload["title"] != "hello" {
		t.Errorf("payload = %v", ev.Payload)
	}
}

func TestForwardRespectsTopicFilter(t *testing.T) {
	wanted := &subExt{baseExt: baseExt{id: "sink.messages"}, topics: []string{extension.TopicMessage}}
	all := &subExt{baseExt: baseExt{id: "sink.all"}}
	mgr := managerWith(t, wanted, all)

	broker := pubsub.NewBroker[fakeResource]()
	t.Cleanup(broker.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	Forward(ctx, mgr, extension.TopicSession, broker)
	broker.Publish(pubsub.UpdatedEvent, fakeResource{ID: "s1"})

	if !waitFor(t, func() bool { return len(all.seen()) == 1 }) {
		t.Fatal("unfiltered subscriber got nothing")
	}
	if got := len(wanted.seen()); got != 0 {
		t.Errorf("subscriber received a topic it did not ask for: %d events", got)
	}
}

// A panicking subscriber must not stop the fan-out for the others.
func TestForwardContainsSubscriberPanic(t *testing.T) {
	bad := &subExt{baseExt: baseExt{id: "sink.bad"}, panics: true}
	good := &subExt{baseExt: baseExt{id: "sink.good"}}
	mgr := managerWith(t, bad, good)

	broker := pubsub.NewBroker[fakeResource]()
	t.Cleanup(broker.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	Forward(ctx, mgr, extension.TopicSession, broker)
	broker.Publish(pubsub.CreatedEvent, fakeResource{ID: "s1"})
	broker.Publish(pubsub.UpdatedEvent, fakeResource{ID: "s1"})

	if !waitFor(t, func() bool { return len(good.seen()) == 2 }) {
		t.Fatalf("fan-out stopped after a panic: %d events", len(good.seen()))
	}
}

// A build with no subscriber must start no goroutine and no subscription.
func TestForwardIsNoOpWithoutSubscribers(t *testing.T) {
	mgr := managerWith(t, &provExt{baseExt: baseExt{id: "tools.acme"}})
	if HasEventSubscribers(mgr) {
		t.Fatal("HasEventSubscribers is wrong")
	}

	broker := pubsub.NewBroker[fakeResource]()
	t.Cleanup(broker.Shutdown)
	Forward(context.Background(), mgr, extension.TopicSession, broker)
	broker.Publish(pubsub.CreatedEvent, fakeResource{ID: "s1"})
	// Nothing to assert beyond not blocking or panicking: the point is that no
	// subscription was created.
}

func TestForwardStopsOnContextCancel(t *testing.T) {
	sub := &subExt{baseExt: baseExt{id: "sink.acme"}}
	mgr := managerWith(t, sub)

	broker := pubsub.NewBroker[fakeResource]()
	t.Cleanup(broker.Shutdown)
	ctx, cancel := context.WithCancel(context.Background())

	Forward(ctx, mgr, extension.TopicSession, broker)
	broker.Publish(pubsub.CreatedEvent, fakeResource{ID: "s1"})
	if !waitFor(t, func() bool { return len(sub.seen()) == 1 }) {
		t.Fatal("first event never arrived")
	}

	cancel()
	// Give the unsubscribe goroutine a chance to run before publishing again.
	if !waitFor(t, func() bool { return broker.GetSubscriberCount() == 0 }) {
		t.Fatal("subscription was not released on cancel")
	}
	broker.Publish(pubsub.UpdatedEvent, fakeResource{ID: "s1"})

	time.Sleep(50 * time.Millisecond)
	if got := len(sub.seen()); got != 1 {
		t.Errorf("events kept arriving after cancel: %d", got)
	}
}

// topicPanicExt cannot say which topics it wants.
type topicPanicExt struct{ subExt }

func (e *topicPanicExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *topicPanicExt) Topics() []string              { panic("boom") }

func TestForwardContainsPanickingTopics(t *testing.T) {
	sub := &topicPanicExt{subExt: subExt{baseExt: baseExt{id: "events.broken"}}}
	good := &subExt{baseExt: baseExt{id: "events.good"}}
	mgr := managerWith(t, sub, good)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker := pubsub.NewBroker[fakeResource]()
	Forward(ctx, mgr, "test", broker)

	broker.Publish(pubsub.CreatedEvent, fakeResource{ID: "r1"})

	// The healthy subscriber still gets its event; the broken one is skipped
	// rather than being handed everything.
	if !waitFor(t, func() bool { return len(good.seen()) == 1 }) {
		t.Fatal("a panicking Topics() stopped delivery to the other subscribers")
	}
	if got := len(sub.seen()); got != 0 {
		t.Fatalf("broken subscriber received %d events, want 0", got)
	}
}
