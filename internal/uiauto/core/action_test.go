package core

import (
	"context"
	"testing"
)

// fakeBackend is a minimal, configurable Backend used to test ActionResolver
// without any OS dependency.
type fakeBackend struct {
	// failKinds maps an ActionKind to the error it should return from
	// Perform; a kind absent from the map succeeds.
	failKinds map[ActionKind]error
	// performed records every action requested, in order.
	performed []ActionKind
}

func newFakeBackend(failKinds map[ActionKind]error) *fakeBackend {
	return &fakeBackend{failKinds: failKinds}
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Available(ctx context.Context) (Capabilities, error) {
	return Capabilities{UIActions: true}, nil
}
func (f *fakeBackend) Apps(ctx context.Context) ([]AppInfo, error) { return nil, nil }
func (f *fakeBackend) Windows(ctx context.Context, appID string) ([]WindowInfo, error) {
	return nil, nil
}
func (f *fakeBackend) Find(ctx context.Context, scope Scope, sel *Selector, limit int) ([]*Element, error) {
	return nil, nil
}
func (f *fakeBackend) Children(ctx context.Context, el *Element) ([]*Element, error) { return nil, nil }
func (f *fakeBackend) Properties(ctx context.Context, el *Element, props []string) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBackend) Perform(ctx context.Context, el *Element, action Action) error {
	f.performed = append(f.performed, action.Kind)
	if err, ok := f.failKinds[action.Kind]; ok {
		return err
	}
	return nil
}
func (f *fakeBackend) Close() error { return nil }

// fakePhysical is a minimal, configurable PhysicalInput used to test
// ActionResolver fallback paths.
type fakePhysical struct {
	failClick  error
	failType   error
	failPress  error
	failScroll error
	clicked    []struct{ x, y int }
	typed      []string
	pressed    []string
	scrolled   []struct{ x, y, amount int }
}

func (p *fakePhysical) Click(x, y int) error {
	p.clicked = append(p.clicked, struct{ x, y int }{x, y})
	return p.failClick
}
func (p *fakePhysical) MoveMouse(x, y int) error { return nil }
func (p *fakePhysical) TypeText(s string) error {
	p.typed = append(p.typed, s)
	return p.failType
}
func (p *fakePhysical) PressKey(key string) error {
	p.pressed = append(p.pressed, key)
	return p.failPress
}
func (p *fakePhysical) Scroll(x, y, amount int) error {
	p.scrolled = append(p.scrolled, struct{ x, y, amount int }{x, y, amount})
	return p.failScroll
}

func elWithBounds() *Element {
	return &Element{
		ID:      FormatElementRef("s1", "e1"),
		Role:    RoleButton,
		Name:    "Save",
		Bounds:  Bounds{X: 10, Y: 20, W: 40, H: 10},
		Enabled: true,
		Visible: true,
	}
}

func TestActionResolver_Click_Native(t *testing.T) {
	backend := newFakeBackend(nil)
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Click(context.Background(), elWithBounds())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "native" {
		t.Fatalf("expected native method, got %s", res.Method)
	}
	if len(physical.clicked) != 0 {
		t.Fatalf("expected no physical click, got %d", len(physical.clicked))
	}
}

func TestActionResolver_Click_FallbackToPhysical(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionInvoke: NewActionFailedError("invoke unsupported")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Click(context.Background(), elWithBounds())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical method, got %s", res.Method)
	}
	if len(physical.clicked) != 1 {
		t.Fatalf("expected exactly 1 physical click, got %d", len(physical.clicked))
	}
	if physical.clicked[0].x != 30 || physical.clicked[0].y != 25 {
		t.Fatalf("expected click at bounds center (30,25), got (%d,%d)", physical.clicked[0].x, physical.clicked[0].y)
	}
	if len(res.Notes) == 0 {
		t.Fatalf("expected a fallback note to be recorded")
	}
}

func TestActionResolver_Click_NoFallbackWhenPhysicalDisallowed(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionInvoke: NewActionFailedError("invoke unsupported")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, false)

	_, err := resolver.Click(context.Background(), elWithBounds())
	if err == nil {
		t.Fatalf("expected error when physical fallback is disallowed")
	}
	if len(physical.clicked) != 0 {
		t.Fatalf("expected no physical click when disallowed")
	}
}

func TestActionResolver_Click_NoFallbackWhenBoundsEmpty(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionInvoke: NewActionFailedError("invoke unsupported")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	el := elWithBounds()
	el.Bounds = Bounds{}
	_, err := resolver.Click(context.Background(), el)
	if err == nil {
		t.Fatalf("expected error when bounds are empty and no fallback is possible")
	}
}

func TestActionResolver_Click_PropagatesNonFallbackError(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionInvoke: NewPermDeniedError("blocked")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	_, err := resolver.Click(context.Background(), elWithBounds())
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrPermDenied {
		t.Fatalf("expected PERM_DENIED to propagate untouched, got %v", err)
	}
	if len(physical.clicked) != 0 {
		t.Fatalf("expected no physical fallback for a non-fallback-eligible error")
	}
}

func TestActionResolver_Type_NativeSetValue(t *testing.T) {
	backend := newFakeBackend(nil)
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Type(context.Background(), elWithBounds(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "native" || res.Action.Kind != ActionSetValue {
		t.Fatalf("expected native setvalue, got %+v", res)
	}
}

func TestActionResolver_Type_FallbackChain(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{
		ActionSetValue: NewActionFailedError("no setvalue pattern"),
		ActionType:     NewPlatformNotSupportedError("no type action"),
	})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Type(context.Background(), elWithBounds(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical fallback, got %s", res.Method)
	}
	if len(physical.typed) != 1 || physical.typed[0] != "hello" {
		t.Fatalf("expected physical typing of 'hello', got %+v", physical.typed)
	}
}

func TestActionResolver_Scroll_FallbackToPhysical(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionScroll: NewActionFailedError("no scroll pattern")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Scroll(context.Background(), elWithBounds(), -3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical scroll fallback, got %s", res.Method)
	}
	if len(physical.scrolled) != 1 || physical.scrolled[0].amount != -3 {
		t.Fatalf("expected physical scroll with amount -3, got %+v", physical.scrolled)
	}
}

func TestActionResolver_Focus_FallbackToPhysical(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionFocus: NewActionFailedError("no focus pattern")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Focus(context.Background(), elWithBounds())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical focus fallback, got %s", res.Method)
	}
	if len(physical.clicked) != 1 {
		t.Fatalf("expected exactly one physical click to focus, got %d", len(physical.clicked))
	}
}

func TestActionResolver_Press_TargetedFallback(t *testing.T) {
	backend := newFakeBackend(map[ActionKind]error{ActionPress: NewActionFailedError("no press pattern")})
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Press(context.Background(), elWithBounds(), "Enter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical press fallback, got %s", res.Method)
	}
	if len(physical.pressed) != 1 || physical.pressed[0] != "Enter" {
		t.Fatalf("expected physical press of Enter, got %+v", physical.pressed)
	}
}

func TestActionResolver_Press_GlobalNoElement(t *testing.T) {
	backend := newFakeBackend(nil)
	physical := &fakePhysical{}
	resolver := NewActionResolver(backend, physical, true)

	res, err := resolver.Press(context.Background(), nil, "Escape")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Method != "physical" {
		t.Fatalf("expected physical for a global key press, got %s", res.Method)
	}
	if len(backend.performed) != 0 {
		t.Fatalf("expected backend not to be consulted for a global (nil-element) press")
	}
}

func TestActionResolver_Press_GlobalNoPhysical(t *testing.T) {
	backend := newFakeBackend(nil)
	resolver := NewActionResolver(backend, nil, true)

	_, err := resolver.Press(context.Background(), nil, "Escape")
	de, ok := AsDesktopError(err)
	if !ok || de.Code != ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED, got %v", err)
	}
}
