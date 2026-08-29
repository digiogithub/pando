package linux

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/events"
	"github.com/godbus/dbus/v5"
)

func sig(name, sender string, path dbus.ObjectPath, body ...interface{}) *dbus.Signal {
	return &dbus.Signal{Name: name, Sender: sender, Path: path, Body: body}
}

func TestDecodeAtspiEventIgnoresOtherInterfaces(t *testing.T) {
	s := sig("org.freedesktop.DBus.NameOwnerChanged", ":1.1", "/some/path")
	if _, ok := decodeAtspiEvent(s); ok {
		t.Fatal("expected a non-Event.Object signal to be ignored")
	}
}

func TestDecodeAtspiEventUnknownMember(t *testing.T) {
	s := sig(atspiEventIface+".SomeFutureMember", ":1.1", "/some/path")
	if _, ok := decodeAtspiEvent(s); ok {
		t.Fatal("expected an unrecognized member to be ignored, not errored")
	}
}

func TestDecodeAtspiEventChildrenChangedAdd(t *testing.T) {
	s := sig(atspiEventIface+".ChildrenChanged", ":1.5", "/org/a11y/atspi/accessible/42", "add", int32(0), int32(0), dbus.MakeVariant(""))
	ev, ok := decodeAtspiEvent(s)
	if !ok {
		t.Fatal("expected ChildrenChanged/add to decode")
	}
	if ev.Kind != events.KindCreated {
		t.Fatalf("Kind = %q, want %q", ev.Kind, events.KindCreated)
	}
	if ev.WindowID != ":1.5/org/a11y/atspi/accessible/42" {
		t.Fatalf("WindowID = %q", ev.WindowID)
	}
	if ev.Details["member"] != "ChildrenChanged" {
		t.Fatalf("Details[member] = %v", ev.Details["member"])
	}
}

func TestDecodeAtspiEventChildrenChangedRemove(t *testing.T) {
	s := sig(atspiEventIface+".ChildrenChanged", ":1.5", "/x", "remove", int32(0), int32(0), dbus.MakeVariant(""))
	ev, ok := decodeAtspiEvent(s)
	if !ok || ev.Kind != events.KindDestroyed {
		t.Fatalf("expected KindDestroyed, got ok=%v kind=%q", ok, ev.Kind)
	}
}

func TestDecodeAtspiEventStateChangedFocused(t *testing.T) {
	s := sig(atspiEventIface+".StateChanged", ":1.5", "/x", "focused", int32(1), int32(0), dbus.MakeVariant(""))
	ev, ok := decodeAtspiEvent(s)
	if !ok || ev.Kind != events.KindFocusChanged {
		t.Fatalf("expected KindFocusChanged, got ok=%v kind=%q", ok, ev.Kind)
	}
}

func TestDecodeAtspiEventStateChangedOther(t *testing.T) {
	s := sig(atspiEventIface+".StateChanged", ":1.5", "/x", "sensitive", int32(1), int32(0), dbus.MakeVariant(""))
	ev, ok := decodeAtspiEvent(s)
	if !ok || ev.Kind != events.KindPropertyChanged {
		t.Fatalf("expected KindPropertyChanged, got ok=%v kind=%q", ok, ev.Kind)
	}
}

func TestDecodeAtspiEventPropertyChangeValue(t *testing.T) {
	s := sig(atspiEventIface+".PropertyChange", ":1.5", "/x", "accessible-value", int32(0), int32(0), dbus.MakeVariant(""))
	ev, ok := decodeAtspiEvent(s)
	if !ok || ev.Kind != events.KindValueChanged {
		t.Fatalf("expected KindValueChanged, got ok=%v kind=%q", ok, ev.Kind)
	}
}

func TestDecodeAtspiEventPropertyChangeOther(t *testing.T) {
	s := sig(atspiEventIface+".PropertyChange", ":1.5", "/x", "accessible-name", int32(0), int32(0), dbus.MakeVariant(""))
	ev, ok := decodeAtspiEvent(s)
	if !ok || ev.Kind != events.KindPropertyChanged {
		t.Fatalf("expected KindPropertyChanged, got ok=%v kind=%q", ok, ev.Kind)
	}
}

func TestDecodeAtspiEventTextAndValueChanged(t *testing.T) {
	for _, member := range []string{"TextChanged", "ValueChanged"} {
		s := sig(atspiEventIface+"."+member, ":1.5", "/x")
		ev, ok := decodeAtspiEvent(s)
		if !ok || ev.Kind != events.KindValueChanged {
			t.Fatalf("%s: expected KindValueChanged, got ok=%v kind=%q", member, ok, ev.Kind)
		}
	}
}

func TestDecodeAtspiEventNilSignal(t *testing.T) {
	if _, ok := decodeAtspiEvent(nil); ok {
		t.Fatal("expected nil signal to be ignored")
	}
}

func TestFirstStringArgEmptyBody(t *testing.T) {
	s := sig(atspiEventIface+".StateChanged", ":1.5", "/x")
	got, ok := firstStringArg(s)
	if ok || got != "" {
		t.Fatalf("expected (\"\", false) for empty body, got (%q, %v)", got, ok)
	}
}

func TestFirstStringArgNonStringFirstArg(t *testing.T) {
	s := sig(atspiEventIface+".StateChanged", ":1.5", "/x", int32(42))
	got, ok := firstStringArg(s)
	if ok || got != "" {
		t.Fatalf("expected (\"\", false) for non-string first arg, got (%q, %v)", got, ok)
	}
}

// TestEventSourceEnsureStartedRequiresConn asserts the honest failure
// path: no connection means PLATFORM_NOT_SUPPORTED, never a nil-pointer
// panic.
func TestEventSourceEnsureStartedRequiresConn(t *testing.T) {
	src := newEventSource()
	if err := src.ensureStarted(nil); err == nil {
		t.Fatal("expected an error when conn is nil")
	}
	if err := src.ensureStarted(&dbusConn{}); err == nil {
		t.Fatal("expected an error when conn.conn is nil")
	}
}

func TestEventSourceCloseIsIdempotentBeforeStart(t *testing.T) {
	src := newEventSource()
	src.close()
	src.close() // must not panic
}

// TestIntegrationAtspiSubscribeLiveBus is a real-bus smoke test: it
// installs a real D-Bus match rule and confirms Subscribe does not error
// and the fan-out channel behaves (no event required -- this dev box has
// no a11y-registered GUI apps running, so asserting a specific event would
// be flaky/impossible; the goal is to prove ensureStarted's wiring against
// a genuine *dbus.Conn works, matching the honesty bar backend_integration_
// test.go already sets for this package).
func TestIntegrationAtspiSubscribeLiveBus(t *testing.T) {
	skipUnlessA11yBusReachable(t)

	b := &AtspiBackend{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, unsub, err := b.Subscribe(ctx, core.Scope{})
	if err != nil {
		t.Fatalf("Subscribe failed against a live bus: %v", err)
	}
	defer unsub()
	if ch == nil {
		t.Fatal("expected a non-nil event channel")
	}
	// Subscribing again must reuse the same eventSource (no double D-Bus
	// match rule installed) rather than erroring.
	ch2, unsub2, err := b.Subscribe(ctx, core.Scope{})
	if err != nil {
		t.Fatalf("second Subscribe failed: %v", err)
	}
	defer unsub2()
	if ch2 == nil {
		t.Fatal("expected a non-nil second event channel")
	}
	_ = b.Close()
}
