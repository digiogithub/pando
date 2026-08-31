package linux

import (
	"context"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/godbus/dbus/v5"
)

const (
	a11yBusDest     = "org.a11y.Bus"
	a11yBusPath     = dbus.ObjectPath("/org/a11y/bus")
	a11yBusIface    = "org.a11y.Bus"
	registryDest    = "org.a11y.atspi.Registry"
	registryPath    = dbus.ObjectPath("/org/a11y/atspi/accessible/root")
	accessibleIface = "org.a11y.atspi.Accessible"
	componentIface  = "org.a11y.atspi.Component"
	actionIface     = "org.a11y.atspi.Action"
	textIface       = "org.a11y.atspi.Text"
	editTextIface   = "org.a11y.atspi.EditableText"
	valueIface      = "org.a11y.atspi.Value"
	selectionIface  = "org.a11y.atspi.Selection"
)

const accessibilityDisabledSuggestion = "Enable assistive technologies for the desktop session (e.g. " +
	"`gsettings set org.gnome.desktop.interface toolkit-accessibility true`, or the equivalent " +
	"Accessibility setting in your desktop environment), then restart the application you want to " +
	"automate so it registers with AT-SPI."

// discoverBusAddress resolves the AT-SPI2 accessibility bus address: first
// by asking org.a11y.Bus.GetAddress on the session bus, then by falling
// back to the AT_SPI_BUS_ADDRESS environment variable (set by some desktop
// environments / sandboxes that do not run a per-session a11y bus broker).
func discoverBusAddress(ctx context.Context) (string, error) {
	if addr, err := discoverBusAddressViaSessionBus(ctx); err == nil && addr != "" {
		return addr, nil
	}
	if addr := strings.TrimSpace(os.Getenv("AT_SPI_BUS_ADDRESS")); addr != "" {
		return addr, nil
	}
	return "", core.NewPermDeniedError(
		"could not discover the AT-SPI2 accessibility bus address (org.a11y.Bus.GetAddress failed " +
			"and AT_SPI_BUS_ADDRESS is not set); accessibility does not appear to be enabled for this session")
}

func discoverBusAddressViaSessionBus(ctx context.Context) (string, error) {
	session, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer session.Close()

	obj := session.Object(a11yBusDest, a11yBusPath)
	call := obj.CallWithContext(ctx, a11yBusIface+".GetAddress", 0)
	if call.Err != nil {
		return "", call.Err
	}
	var addr string
	if err := call.Store(&addr); err != nil {
		return "", err
	}
	return strings.TrimSpace(addr), nil
}

// connectA11yBus discovers and dials the AT-SPI2 accessibility bus,
// returning a ready-to-use dbusConn. Failure is always reported as a
// PERM_DENIED/PLATFORM_NOT_SUPPORTED core.DesktopError with an actionable
// suggestion, never a bare Go error.
//
// ctx bounds the discovery and the dial only. The returned connection
// deliberately does NOT inherit its cancellation: dbus.WithContext binds
// the CONNECTION's lifetime to the context it is handed, and callers pass
// a per-operation context here (AtspiBackend.ensureConn caches the result
// across every later call). Inheriting it killed the cached connection the
// moment the first operation's deadline elapsed, so every subsequent
// AT-SPI call failed with "use of closed network connection".
func connectA11yBus(ctx context.Context) (*dbusConn, error) {
	addr, err := discoverBusAddress(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := dbus.Connect(addr, dbus.WithContext(context.WithoutCancel(ctx)))
	if err != nil {
		de := core.NewPermDeniedError("failed to connect to the AT-SPI2 accessibility bus at " + addr + ": " + err.Error())
		de.Suggestion = accessibilityDisabledSuggestion
		return nil, de
	}
	return newDbusConn(conn), nil
}

// sessionKind reports the detected windowing session type, from the
// standard XDG_SESSION_TYPE/WAYLAND_DISPLAY/DISPLAY environment variables.
// It never guesses beyond what the environment actually tells it.
func sessionKind() string {
	if k := strings.ToLower(strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE"))); k == "wayland" || k == "x11" {
		return k
	}
	if strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) != "" {
		return "wayland"
	}
	if strings.TrimSpace(os.Getenv("DISPLAY")) != "" {
		return "x11"
	}
	return "unknown"
}

// detectCapabilities reports honestly what this backend can do in the
// current session: Accessibility/UIInspection/UIActions reflect whether the
// a11y bus actually answered; Mouse/Keyboard/Screenshot are always false
// here — physical input and screen capture are Phase 3's
// internal/uiauto/input and internal/uiauto/screen packages, not this one.
// Events reflects the same bus reachability: AtspiBackend implements
// events.Subscriber over real org.a11y.atspi.Event.Object D-Bus signals
// (events.go), so a reachable bus genuinely supports it. WindowManagement
// is not implemented by this backend.
func detectCapabilities(busAvailable bool) core.Capabilities {
	return core.Capabilities{
		Accessibility: busAvailable,
		UIInspection:  busAvailable,
		UIActions:     busAvailable,
		Events:        busAvailable,
	}
}
