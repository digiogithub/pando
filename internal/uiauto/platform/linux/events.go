package linux

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/events"
	"github.com/godbus/dbus/v5"
)

// atspiEventIface is the D-Bus interface every AT-SPI2 object-level
// accessibility event signal is emitted under. The member name after the
// last dot ("PropertyChange", "StateChanged", "ChildrenChanged", ...)
// distinguishes the concrete event; most of these signals additionally
// carry a "detail1" string first argument that further refines what
// changed (see decodeAtspiEvent/atspiMemberKind below), per the AT-SPI2
// D-Bus specification (org.a11y.atspi.Event.Object).
const atspiEventIface = "org.a11y.atspi.Event.Object"

// eventSource lazily installs exactly one D-Bus signal match + dispatch
// goroutine per AtspiBackend, fanning every decoded events.Event out
// through one events.EventBus. This means any number of concurrent
// Manager.Wait callers share a single underlying subscription instead of
// each installing their own match rule (a D-Bus match rule and the
// associated dispatch goroutine are not free, and this mirrors the same
// "one listener, many waiters" design internal/uiauto/platform/browser
// uses for CDP).
type eventSource struct {
	mu      sync.Mutex
	started bool
	bus     *events.EventBus
	cancel  context.CancelFunc
}

func newEventSource() *eventSource {
	return &eventSource{bus: events.NewEventBus()}
}

// ensureStarted installs the org.a11y.atspi.Event.Object match rule on
// conn (idempotent -- a no-op once already started) and starts the
// dispatch goroutine that decodes and republishes signals through s.bus.
func (s *eventSource) ensureStarted(conn *dbusConn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	if conn == nil || conn.conn == nil {
		return core.NewPlatformNotSupportedError("no AT-SPI2 bus connection available to subscribe events on")
	}

	signalCh := make(chan *dbus.Signal, 64)
	conn.conn.Signal(signalCh)
	if err := conn.conn.AddMatchSignal(dbus.WithMatchInterface(atspiEventIface)); err != nil {
		return core.NewPlatformNotSupportedError("failed to subscribe to AT-SPI2 accessibility events: " + err.Error())
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-signalCh:
				if !ok {
					return
				}
				if ev, ok := decodeAtspiEvent(sig); ok {
					s.bus.Publish(ev)
				}
			}
		}
	}()
	s.started = true
	return nil
}

// close stops the dispatch goroutine and closes every fanned-out
// subscriber channel. Safe to call even if ensureStarted was never
// called.
func (s *eventSource) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.bus.Close()
	s.started = false
}

// decodeAtspiEvent maps one org.a11y.atspi.Event.Object.* D-Bus signal
// onto an events.Event. It never fails loudly: a signal this package does
// not recognize is simply dropped (ok=false), never propagated as an
// error, since a stray/unexpected signal must not tear down the whole
// subscription.
func decodeAtspiEvent(sig *dbus.Signal) (events.Event, bool) {
	if sig == nil || !strings.HasPrefix(string(sig.Name), atspiEventIface+".") {
		return events.Event{}, false
	}
	member := strings.TrimPrefix(string(sig.Name), atspiEventIface+".")
	kind, ok := atspiMemberKind(member, sig)
	if !ok {
		return events.Event{}, false
	}
	ref := accessibleRef{Bus: sig.Sender, Path: sig.Path}
	return events.Event{
		Kind: kind,
		// AT-SPI object identity (bus+path) is not a Manager snapshot
		// ref -- events.WaitFor always re-resolves the locator's
		// selector against the backend rather than trusting this field,
		// so leaving ElementRef empty here is correct, not a gap.
		WindowID:  ref.String(),
		Timestamp: time.Now(),
		Details: map[string]any{
			"member": member,
			"bus":    sig.Sender,
			"path":   string(sig.Path),
		},
	}, true
}

// atspiMemberKind maps one AT-SPI2 Event.Object member (plus, where
// needed, its detail1 string argument) onto an events.Kind.
func atspiMemberKind(member string, sig *dbus.Signal) (events.Kind, bool) {
	detail1, _ := firstStringArg(sig)
	switch member {
	case "ChildrenChanged":
		if strings.HasPrefix(detail1, "remove") {
			return events.KindDestroyed, true
		}
		return events.KindCreated, true
	case "StateChanged":
		if detail1 == "focused" {
			return events.KindFocusChanged, true
		}
		return events.KindPropertyChanged, true
	case "PropertyChange":
		if detail1 == "accessible-value" {
			return events.KindValueChanged, true
		}
		return events.KindPropertyChanged, true
	case "TextChanged", "ValueChanged":
		return events.KindValueChanged, true
	default:
		return "", false
	}
}

func firstStringArg(sig *dbus.Signal) (string, bool) {
	if sig == nil || len(sig.Body) == 0 {
		return "", false
	}
	s, ok := sig.Body[0].(string)
	return s, ok
}

// Subscribe implements events.Subscriber for AtspiBackend: it lazily
// connects to the a11y bus (same as every other operation), lazily starts
// the shared eventSource, and hands the caller a fresh fan-out channel
// from its EventBus. scope is currently not used to filter server-side
// (AT-SPI2's Event.Object signals are not natively scopable that way);
// events.WaitFor always re-evaluates the actual locator/condition against
// the backend on every received event, so an unrelated event only costs
// one harmless extra Find call, never an incorrect result.
func (b *AtspiBackend) Subscribe(ctx context.Context, scope core.Scope) (<-chan events.Event, func(), error) {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return nil, nil, err
	}

	b.mu.Lock()
	if b.events == nil {
		b.events = newEventSource()
	}
	src := b.events
	b.mu.Unlock()

	if err := src.ensureStarted(conn); err != nil {
		return nil, nil, err
	}
	ch, unsub := src.bus.Subscribe(32)
	return ch, unsub, nil
}
