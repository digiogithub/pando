package linux

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// TestDiscoverBusAddressFallsBackToEnv forces the session-bus lookup to
// fail deterministically (an unreachable DBUS_SESSION_BUS_ADDRESS) and
// verifies discoverBusAddress falls back to AT_SPI_BUS_ADDRESS.
func TestDiscoverBusAddressFallsBackToEnv(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/definitely/not/here")
	t.Setenv("AT_SPI_BUS_ADDRESS", "unix:path=/fake/a11y/bus")

	addr, err := discoverBusAddress(context.Background())
	if err != nil {
		t.Fatalf("expected the AT_SPI_BUS_ADDRESS fallback to succeed, got error: %v", err)
	}
	if addr != "unix:path=/fake/a11y/bus" {
		t.Fatalf("expected the fallback address, got %q", addr)
	}
}

// TestDiscoverBusAddressReportsPermDenied verifies that when both the
// session-bus lookup and the AT_SPI_BUS_ADDRESS fallback are unavailable,
// discoverBusAddress returns an actionable PERM_DENIED core.DesktopError
// rather than a bare Go error.
func TestDiscoverBusAddressReportsPermDenied(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/definitely/not/here")
	t.Setenv("AT_SPI_BUS_ADDRESS", "")

	_, err := discoverBusAddress(context.Background())
	if err == nil {
		t.Fatalf("expected an error when neither the session bus nor AT_SPI_BUS_ADDRESS are reachable")
	}
	de, ok := core.AsDesktopError(err)
	if !ok {
		t.Fatalf("expected a *core.DesktopError, got %T: %v", err, err)
	}
	if de.Code != core.ErrPermDenied {
		t.Fatalf("expected PERM_DENIED, got %s", de.Code)
	}
	if de.Suggestion == "" {
		t.Fatalf("expected an actionable suggestion")
	}
}

// TestConnectA11yBusMapsFailureToDesktopError verifies connectA11yBus wraps
// a dial failure as an actionable PERM_DENIED core.DesktopError with the
// "enable assistive technologies" suggestion.
func TestConnectA11yBusMapsFailureToDesktopError(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/definitely/not/here")
	t.Setenv("AT_SPI_BUS_ADDRESS", "unix:path=/also/nonexistent")

	_, err := connectA11yBus(context.Background())
	if err == nil {
		t.Fatalf("expected connectA11yBus to fail against an unreachable address")
	}
	de, ok := core.AsDesktopError(err)
	if !ok {
		t.Fatalf("expected a *core.DesktopError, got %T: %v", err, err)
	}
	if de.Code != core.ErrPermDenied {
		t.Fatalf("expected PERM_DENIED, got %s", de.Code)
	}
	if de.Suggestion != accessibilityDisabledSuggestion {
		t.Fatalf("expected the accessibility-disabled suggestion, got %q", de.Suggestion)
	}
}

func TestSessionKindDetection(t *testing.T) {
	cases := []struct {
		xdgType, wayland, display, want string
	}{
		{"wayland", "", "", "wayland"},
		{"x11", "", "", "x11"},
		{"", "wayland-0", "", "wayland"},
		{"", "", ":0", "x11"},
		{"", "", "", "unknown"},
	}
	for _, c := range cases {
		t.Setenv("XDG_SESSION_TYPE", c.xdgType)
		t.Setenv("WAYLAND_DISPLAY", c.wayland)
		t.Setenv("DISPLAY", c.display)
		if got := sessionKind(); got != c.want {
			t.Errorf("sessionKind() with XDG_SESSION_TYPE=%q WAYLAND_DISPLAY=%q DISPLAY=%q = %q, want %q",
				c.xdgType, c.wayland, c.display, got, c.want)
		}
	}
}

func TestDetectCapabilitiesNeverFakesInputOrScreenshot(t *testing.T) {
	caps := detectCapabilities(true)
	if !caps.Accessibility || !caps.UIInspection || !caps.UIActions {
		t.Fatalf("expected Accessibility/UIInspection/UIActions true when the bus answered, got %+v", caps)
	}
	if caps.Mouse || caps.Keyboard || caps.Screenshot {
		t.Fatalf("expected Mouse/Keyboard/Screenshot to stay false — those are Phase 3's job, got %+v", caps)
	}

	caps = detectCapabilities(false)
	if caps.Accessibility || caps.UIInspection || caps.UIActions {
		t.Fatalf("expected all-false capabilities when the bus is unreachable, got %+v", caps)
	}
}
