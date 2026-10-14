package config

import "testing"

func TestSubscribeFuncRunsSynchronouslyAndUnsubscribes(t *testing.T) {
	b := newEventBus()
	var got []string
	unsubscribe := b.SubscribeFunc(func(ev ConfigChangeEvent) { got = append(got, ev.Section) })

	ch := make(chan ConfigChangeEvent, 1)
	b.Subscribe(ch)

	b.Publish(ConfigChangeEvent{Section: "decisionModel"})
	// The listener has already run when Publish returns.
	if len(got) != 1 || got[0] != "decisionModel" {
		t.Fatalf("listener calls = %v, want [decisionModel]", got)
	}
	if ev := <-ch; ev.Section != "decisionModel" {
		t.Fatalf("channel subscriber got %q", ev.Section)
	}

	unsubscribe()
	b.Publish(ConfigChangeEvent{Section: "agents"})
	if len(got) != 1 {
		t.Fatalf("listener ran after unsubscribe: %v", got)
	}
}

func TestSubscribeFuncListenerMayTouchTheBus(t *testing.T) {
	b := newEventBus()
	// Listeners run outside the bus lock, so registering another subscriber
	// from inside one must not deadlock.
	b.SubscribeFunc(func(ConfigChangeEvent) { b.Subscribe(make(chan ConfigChangeEvent)) })
	b.Publish(ConfigChangeEvent{Section: "agents"})
}
