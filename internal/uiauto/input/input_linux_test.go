//go:build linux

package input

import (
	"os"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestSessionTypeNone(t *testing.T) {
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("this test only asserts the no-display-session honesty path; DISPLAY/WAYLAND_DISPLAY is set here")
	}
	if got := sessionType(); got != "none" {
		t.Fatalf("sessionType() = %q, want %q", got, "none")
	}
	caps := Capabilities()
	if caps.Mouse || caps.Keyboard {
		t.Fatalf("Capabilities() reported input available with no display session: %+v", caps)
	}
	pi, err := New()
	if err != nil {
		t.Fatalf("New() should never fail to construct, got: %v", err)
	}
	if err := pi.Click(0, 0); err == nil {
		t.Fatal("Click() with no display session should fail")
	} else if de, ok := core.AsDesktopError(err); !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("Click() error = %v, want PLATFORM_NOT_SUPPORTED", err)
	}
}

// TestLiveX11Input is a smoke test that only runs when a real X11 (or
// XWayland) display session is available. This development box is a tty
// session with neither DISPLAY nor WAYLAND_DISPLAY set, so it always
// skips here.
func TestLiveX11Input(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X11 DISPLAY available in this environment")
	}
	pi, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := pi.MoveMouse(10, 10); err != nil {
		t.Fatalf("MoveMouse() error: %v", err)
	}
}
