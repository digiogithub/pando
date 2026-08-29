package linux

import (
	"context"
	"os"
	"testing"
	"time"
)

// skipUnlessA11yBusReachable skips t unless a real AT-SPI2 accessibility
// bus is reachable AND a graphical session is present (DISPLAY or
// WAYLAND_DISPLAY set), so this integration test stays green in headless
// CI and in a bare tty session even when an a11y bus daemon happens to be
// registered on the session bus with nothing behind it.
func skipUnlessA11yBusReachable(t *testing.T) *dbusConn {
	t.Helper()
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("skipping AT-SPI integration test: no DISPLAY/WAYLAND_DISPLAY (headless/tty session)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := connectA11yBus(ctx)
	if err != nil {
		t.Skipf("skipping AT-SPI integration test: accessibility bus unreachable: %v", err)
	}
	return conn
}

// TestIntegrationListApplications performs a real smoke run against the
// live AT-SPI2 registry: list the top-level applications it exposes. It is
// intentionally tolerant of an empty result (a graphical session with no
// a11y-registered apps running is a legitimate outcome), it only asserts
// that the round trip itself succeeds without error.
func TestIntegrationListApplications(t *testing.T) {
	conn := skipUnlessA11yBusReachable(t)
	defer conn.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	apps, err := listAppRefs(ctx, conn)
	if err != nil {
		t.Fatalf("listAppRefs against the live a11y bus failed: %v", err)
	}
	t.Logf("live AT-SPI registry reports %d application(s)", len(apps))
	for _, a := range apps {
		n, err := fetchNode(ctx, conn, a)
		if err != nil {
			t.Logf("  %s: fetchNode error: %v", a, err)
			continue
		}
		t.Logf("  %s: name=%q role=%q children=%d", a, n.name, n.roleName, n.childCount)
	}
}

// TestIntegrationBackendAvailable exercises AtspiBackend.Available end to
// end against the live bus (or the null-equivalent honest degrade when
// unreachable — but skipUnlessA11yBusReachable already guarantees a live
// bus here).
func TestIntegrationBackendAvailable(t *testing.T) {
	skipUnlessA11yBusReachable(t) // ensures the precondition; backend dials its own connection below

	backend, err := NewBackend()
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}
	defer backend.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	caps, err := backend.Available(ctx)
	if err != nil {
		t.Fatalf("Available must never return an error, got: %v", err)
	}
	if !caps.Accessibility {
		t.Fatalf("expected Accessibility=true against a live, reachable bus, got %+v", caps)
	}
	if caps.Mouse || caps.Keyboard || caps.Screenshot {
		t.Fatalf("expected Mouse/Keyboard/Screenshot to stay false (Phase 3's job), got %+v", caps)
	}
}
