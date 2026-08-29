package events

import (
	"sync"
	"testing"
	"time"
)

func TestEventBusFanout(t *testing.T) {
	bus := NewEventBus()
	ch1, unsub1 := bus.Subscribe(4)
	ch2, unsub2 := bus.Subscribe(4)
	defer unsub1()
	defer unsub2()

	if got := bus.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}

	bus.Publish(Event{Kind: KindCreated})

	select {
	case ev := <-ch1:
		if ev.Kind != KindCreated {
			t.Fatalf("ch1 got kind %q, want %q", ev.Kind, KindCreated)
		}
	case <-time.After(time.Second):
		t.Fatal("ch1 did not receive the published event")
	}
	select {
	case ev := <-ch2:
		if ev.Kind != KindCreated {
			t.Fatalf("ch2 got kind %q, want %q", ev.Kind, KindCreated)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2 did not receive the published event")
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe(1)
	unsub()
	// Idempotent: calling twice must not panic.
	unsub()

	if got := bus.Len(); got != 0 {
		t.Fatalf("Len() after unsubscribe = %d, want 0", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after unsubscribe")
	}

	// Publishing with no subscribers must not block or panic.
	bus.Publish(Event{Kind: KindDestroyed})
}

func TestEventBusFullBufferDropsRatherThanBlocks(t *testing.T) {
	bus := NewEventBus()
	ch, unsub := bus.Subscribe(1)
	defer unsub()

	done := make(chan struct{})
	go func() {
		// Publish more events than the buffer holds; must never block.
		for i := 0; i < 10; i++ {
			bus.Publish(Event{Kind: KindPropertyChanged})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}

	// Drain whatever made it through; must be at least one, at most the
	// buffer size (no panics, no deadlock).
	select {
	case <-ch:
	default:
		t.Fatal("expected at least one buffered event to have been delivered")
	}
}

func TestEventBusClose(t *testing.T) {
	bus := NewEventBus()
	ch1, _ := bus.Subscribe(1)
	ch2, _ := bus.Subscribe(1)

	bus.Close()
	// Closing twice must not panic.
	bus.Close()

	if _, ok := <-ch1; ok {
		t.Fatal("ch1 should be closed")
	}
	if _, ok := <-ch2; ok {
		t.Fatal("ch2 should be closed")
	}

	// Subscribing after Close must return an already-closed channel, not
	// panic or hang.
	ch3, unsub3 := bus.Subscribe(1)
	if _, ok := <-ch3; ok {
		t.Fatal("subscribing after Close should return an already-closed channel")
	}
	unsub3()
}

func TestEventBusConcurrentSubscribeUnsubscribePublish(t *testing.T) {
	bus := NewEventBus()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				bus.Publish(Event{Kind: KindValueChanged})
			}
		}
	}()

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, unsub := bus.Subscribe(2)
			unsub()
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
