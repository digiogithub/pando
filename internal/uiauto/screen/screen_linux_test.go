//go:build linux

package screen

import (
	"context"
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
	if caps.Screenshot {
		t.Fatalf("Capabilities() reported Screenshot=true with no display session available")
	}
	_, err := Capture(context.Background(), Target{})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("Capture() with no display session = %v, want PLATFORM_NOT_SUPPORTED", err)
	}
}

// TestLiveX11Capture is a smoke test that only runs when a real X11 (or
// XWayland) display session is available; this development box is a tty
// session with neither DISPLAY nor WAYLAND_DISPLAY set, so it always
// skips here. Kept for environments (CI with Xvfb, a real desktop) where
// it can genuinely exercise the GetImage path.
func TestLiveX11Capture(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X11 DISPLAY available in this environment")
	}
	img, err := Capture(context.Background(), Target{})
	if err != nil {
		t.Fatalf("Capture() error: %v", err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("captured image has empty bounds: %v", b)
	}
}
