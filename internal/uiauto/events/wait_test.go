package events

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// fakeBackend is a minimal core.Backend whose Find is driven by a
// caller-supplied func, and which counts how many times Find was called
// (to assert the event-driven path avoids busy polling).
type fakeBackend struct {
	find      func(calls int) ([]*core.Element, error)
	findCalls int32
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Available(ctx context.Context) (core.Capabilities, error) {
	return core.Capabilities{}, nil
}
func (f *fakeBackend) Apps(ctx context.Context) ([]core.AppInfo, error) { return nil, nil }
func (f *fakeBackend) Windows(ctx context.Context, appID string) ([]core.WindowInfo, error) {
	return nil, nil
}
func (f *fakeBackend) Find(ctx context.Context, scope core.Scope, sel *core.Selector, limit int) ([]*core.Element, error) {
	n := int(atomic.AddInt32(&f.findCalls, 1))
	return f.find(n)
}
func (f *fakeBackend) Children(ctx context.Context, el *core.Element) ([]*core.Element, error) {
	return nil, nil
}
func (f *fakeBackend) Properties(ctx context.Context, el *core.Element, props []string) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBackend) Perform(ctx context.Context, el *core.Element, action core.Action) error {
	return nil
}
func (f *fakeBackend) Close() error { return nil }

func (f *fakeBackend) calls() int { return int(atomic.LoadInt32(&f.findCalls)) }

// fakeSubscriber is a controllable events.Subscriber for tests.
type fakeSubscriber struct {
	ch             chan Event
	subscribeCalls int32
	err            error
}

func (s *fakeSubscriber) Subscribe(ctx context.Context, scope core.Scope) (<-chan Event, func(), error) {
	atomic.AddInt32(&s.subscribeCalls, 1)
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.ch, func() {}, nil
}

func newLocator(t *testing.T) *core.Locator {
	t.Helper()
	sel, err := core.ParseSelector(`button[name="OK"]`)
	if err != nil {
		t.Fatalf("ParseSelector: %v", err)
	}
	return core.NewLocator(core.Scope{}, sel)
}

func elementFound() []*core.Element {
	return []*core.Element{{Role: core.RoleButton, Name: "OK", Enabled: true, Visible: true}}
}

func TestWaitForImmediateSatisfaction(t *testing.T) {
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) { return elementFound(), nil }}
	l := newLocator(t)

	el, err := WaitFor(context.Background(), be, nil, l, core.ConditionExists, time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if el == nil || el.Name != "OK" {
		t.Fatalf("unexpected element: %+v", el)
	}
	if be.calls() != 1 {
		t.Fatalf("Find calls = %d, want 1 (immediate check only)", be.calls())
	}
}

func TestWaitForNilSubscriberFallsBackToPolling(t *testing.T) {
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		if n < 3 {
			return nil, core.NewElementNotFoundError("not yet")
		}
		return elementFound(), nil
	}}
	l := newLocator(t)

	el, err := WaitFor(context.Background(), be, nil, l, core.ConditionExists, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if el == nil {
		t.Fatal("expected an element")
	}
	if be.calls() < 3 {
		t.Fatalf("Find calls = %d, want >= 3 (polling fallback)", be.calls())
	}
}

func TestWaitForEventDrivenPathAvoidsPolling(t *testing.T) {
	notFoundThenFound := int32(0)
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		if atomic.LoadInt32(&notFoundThenFound) == 0 {
			return nil, core.NewElementNotFoundError("not yet")
		}
		return elementFound(), nil
	}}
	sub := &fakeSubscriber{ch: make(chan Event, 4)}
	l := newLocator(t)

	go func() {
		time.Sleep(50 * time.Millisecond)
		atomic.StoreInt32(&notFoundThenFound, 1)
		sub.ch <- Event{Kind: KindCreated}
	}()

	start := time.Now()
	el, err := WaitFor(context.Background(), be, sub, l, core.ConditionExists, 5*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if el == nil {
		t.Fatal("expected an element")
	}
	if elapsed > time.Second {
		t.Fatalf("WaitFor took %s, expected it to wake on the event well under 1s", elapsed)
	}
	if atomic.LoadInt32(&sub.subscribeCalls) != 1 {
		t.Fatalf("Subscribe calls = %d, want 1", sub.subscribeCalls)
	}
	// One immediate check + one after the event: the eventFallbackPollInterval
	// (2s) ticker must not have fired in ~50ms.
	if be.calls() > 3 {
		t.Fatalf("Find calls = %d, want a small number (event-driven, not busy polling)", be.calls())
	}
}

func TestWaitForSubscribeErrorFallsBackToPolling(t *testing.T) {
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		if n < 2 {
			return nil, core.NewElementNotFoundError("not yet")
		}
		return elementFound(), nil
	}}
	sub := &fakeSubscriber{err: core.NewPlatformNotSupportedError("no live events on this backend")}
	l := newLocator(t)

	el, err := WaitFor(context.Background(), be, sub, l, core.ConditionExists, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if el == nil {
		t.Fatal("expected an element via polling fallback")
	}
}

func TestWaitForSubscriptionClosedEarlyFallsBackToPolling(t *testing.T) {
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		if n < 3 {
			return nil, core.NewElementNotFoundError("not yet")
		}
		return elementFound(), nil
	}}
	ch := make(chan Event)
	close(ch) // already closed: first receive returns immediately with ok=false
	sub := &fakeSubscriber{ch: ch}
	l := newLocator(t)

	el, err := WaitFor(context.Background(), be, sub, l, core.ConditionExists, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if el == nil {
		t.Fatal("expected an element via polling after the subscription closed")
	}
}

func TestWaitForTimeout(t *testing.T) {
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		return nil, core.NewElementNotFoundError("never")
	}}
	l := newLocator(t)

	_, err := WaitFor(context.Background(), be, nil, l, core.ConditionExists, 100*time.Millisecond)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrTimeout {
		t.Fatalf("err = %v, want TIMEOUT", err)
	}
}

func TestWaitForContextCancellation(t *testing.T) {
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		return nil, core.NewElementNotFoundError("never")
	}}
	sub := &fakeSubscriber{ch: make(chan Event)}
	l := newLocator(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, err := WaitFor(ctx, be, sub, l, core.ConditionExists, 5*time.Second)
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestWaitForNotExistsCondition(t *testing.T) {
	present := int32(1)
	be := &fakeBackend{find: func(n int) ([]*core.Element, error) {
		if atomic.LoadInt32(&present) == 1 {
			return elementFound(), nil
		}
		return nil, core.NewElementNotFoundError("gone")
	}}
	sub := &fakeSubscriber{ch: make(chan Event, 1)}
	l := newLocator(t)

	go func() {
		time.Sleep(30 * time.Millisecond)
		atomic.StoreInt32(&present, 0)
		sub.ch <- Event{Kind: KindDestroyed}
	}()

	el, err := WaitFor(context.Background(), be, sub, l, core.ConditionNotExists, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitFor: %v", err)
	}
	if el != nil {
		t.Fatalf("expected nil element for ConditionNotExists, got %+v", el)
	}
}
