package core

import (
	"context"
	"testing"
	"time"
)

// scriptedBackend is a Backend whose Find results are scripted call-by-call,
// used to exercise Locator/WaitFor without any OS dependency.
type scriptedBackend struct {
	fakeBackend
	// results is consumed in order, one entry per Find call; the last
	// entry is reused once exhausted.
	results [][]*Element
	calls   int
}

func (s *scriptedBackend) Find(ctx context.Context, scope Scope, sel *Selector, limit int) ([]*Element, error) {
	s.calls++
	idx := s.calls - 1
	if idx >= len(s.results) {
		idx = len(s.results) - 1
	}
	if idx < 0 {
		return nil, nil
	}
	res := s.results[idx]
	if len(res) == 0 {
		return nil, NewElementNotFoundError("no match")
	}
	return res, nil
}

func mustSelector(t *testing.T, s string) *Selector {
	t.Helper()
	sel, err := ParseSelector(s)
	if err != nil {
		t.Fatalf("ParseSelector(%q) failed: %v", s, err)
	}
	return sel
}

func TestLocator_Resolve_Found(t *testing.T) {
	el := &Element{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Name: "Save"}
	backend := &scriptedBackend{results: [][]*Element{{el}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Save"]`))

	got, err := loc.Resolve(context.Background(), backend)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != el {
		t.Fatalf("expected resolved element to be the scripted result")
	}
}

func TestLocator_Resolve_NotFound(t *testing.T) {
	backend := &scriptedBackend{results: [][]*Element{{}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Missing"]`))

	_, err := loc.Resolve(context.Background(), backend)
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrElementNotFound {
		t.Fatalf("expected ELEMENT_NOT_FOUND, got %v", err)
	}
}

func TestLocator_ResolveAll(t *testing.T) {
	el1 := &Element{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Name: "A"}
	el2 := &Element{ID: FormatElementRef("s1", "e2"), Role: RoleButton, Name: "B"}
	backend := &scriptedBackend{results: [][]*Element{{el1, el2}}}
	loc := NewLocator(Scope{}, mustSelector(t, "button"))

	got, err := loc.ResolveAll(context.Background(), backend)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(got))
	}
}

func TestWaitFor_ExistsImmediately(t *testing.T) {
	el := &Element{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Name: "Save"}
	backend := &scriptedBackend{results: [][]*Element{{el}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Save"]`))

	got, err := WaitFor(context.Background(), backend, loc, ConditionExists, time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != el {
		t.Fatalf("expected the immediately-resolved element")
	}
	if backend.calls != 1 {
		t.Fatalf("expected exactly one Find call for an immediate match, got %d", backend.calls)
	}
}

func TestWaitFor_ExistsAfterRetries(t *testing.T) {
	el := &Element{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Name: "Save"}
	backend := &scriptedBackend{results: [][]*Element{{}, {}, {el}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Save"]`))

	got, err := WaitFor(context.Background(), backend, loc, ConditionExists, time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != el {
		t.Fatalf("expected element to eventually resolve")
	}
	if backend.calls < 3 {
		t.Fatalf("expected at least 3 Find calls, got %d", backend.calls)
	}
}

func TestWaitFor_NotExists(t *testing.T) {
	backend := &scriptedBackend{results: [][]*Element{{}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Gone"]`))

	_, err := WaitFor(context.Background(), backend, loc, ConditionNotExists, time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitFor_VisibleCondition(t *testing.T) {
	invisible := &Element{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Visible: false}
	visible := &Element{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Visible: true}
	backend := &scriptedBackend{results: [][]*Element{{invisible}, {invisible}, {visible}}}
	loc := NewLocator(Scope{}, mustSelector(t, "button"))

	got, err := WaitFor(context.Background(), backend, loc, ConditionVisible, time.Second, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Visible {
		t.Fatalf("expected the visible element to be returned")
	}
}

func TestWaitFor_Timeout(t *testing.T) {
	backend := &scriptedBackend{results: [][]*Element{{}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Never"]`))

	_, err := WaitFor(context.Background(), backend, loc, ConditionExists, 30*time.Millisecond, 10*time.Millisecond)
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrTimeout {
		t.Fatalf("expected TIMEOUT, got %v", err)
	}
}

func TestWaitFor_ContextCancellation(t *testing.T) {
	backend := &scriptedBackend{results: [][]*Element{{}}}
	loc := NewLocator(Scope{}, mustSelector(t, `button[name="Never"]`))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	_, err := WaitFor(ctx, backend, loc, ConditionExists, 5*time.Second, 10*time.Millisecond)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
