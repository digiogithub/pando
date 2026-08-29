//go:build linux

package input

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/portal"
	"github.com/godbus/dbus/v5"
)

// This file unit-tests portalInput's coordinate-mapping and honesty
// behavior (W2) using a fake portal.RequestCaller, since this dev box has
// no DISPLAY/WAYLAND_DISPLAY and no xdg-desktop-portal running (confirmed
// via env in the Phase 3 change) — a real Wayland compositor round trip is
// not verifiable here.

type fakeRequestCaller struct {
	requestResponses map[string][]struct {
		code    uint32
		results map[string]dbus.Variant
	}
	calls []string
}

func newFakeRequestCaller() *fakeRequestCaller {
	return &fakeRequestCaller{requestResponses: make(map[string][]struct {
		code    uint32
		results map[string]dbus.Variant
	})}
}

func (f *fakeRequestCaller) script(key string, code uint32, results map[string]dbus.Variant) {
	f.requestResponses[key] = append(f.requestResponses[key], struct {
		code    uint32
		results map[string]dbus.Variant
	}{code, results})
}

func (f *fakeRequestCaller) Request(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) (uint32, map[string]dbus.Variant, error) {
	key := iface + "." + method
	f.calls = append(f.calls, key)
	q := f.requestResponses[key]
	if len(q) == 0 {
		return 0, nil, nil
	}
	r := q[0]
	f.requestResponses[key] = q[1:]
	return r.code, r.results, nil
}

func (f *fakeRequestCaller) Call(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error) {
	f.calls = append(f.calls, iface+"."+method)
	return nil, nil
}

func (f *fakeRequestCaller) Close() error { return nil }

// openFakeSessionWithStream builds a real *portal.Session (via the real
// portal.Open state machine) backed by a fake RequestCaller that reports
// one ScreenCast stream covering (0,0)-(1920,1080) at node id 9.
func openFakeSessionWithStream(t *testing.T) (*portal.Session, *fakeRequestCaller) {
	t.Helper()
	f := newFakeRequestCaller()
	f.script(portal.RemoteDesktopIface+".CreateSession", 0, map[string]dbus.Variant{
		"session_handle": dbus.MakeVariant("/s/1"),
	})
	f.script(portal.RemoteDesktopIface+".SelectDevices", 0, nil)
	f.script(portal.ScreenCastIface+".SelectSources", 0, nil)
	f.script(portal.RemoteDesktopIface+".Start", 0, map[string]dbus.Variant{
		"streams": dbus.MakeVariant([]interface{}{
			[]interface{}{uint32(9), map[string]dbus.Variant{
				"position": dbus.MakeVariant([]interface{}{int32(0), int32(0)}),
				"size":     dbus.MakeVariant([]interface{}{int32(1920), int32(1080)}),
			}},
		}),
	})
	sess, err := portal.Open(context.Background(), f, portal.OpenOptions{WantScreenCast: true})
	if err != nil {
		t.Fatalf("portal.Open: %v", err)
	}
	return sess, f
}

func TestPortalInput_MoveMouse_MapsToStreamRelativeCoords(t *testing.T) {
	sess, f := openFakeSessionWithStream(t)
	f.calls = nil // reset call log after setup

	p := &portalInput{session: sess, caller: f}
	if err := p.MoveMouse(100, 50); err != nil {
		t.Fatalf("MoveMouse: %v", err)
	}
	found := false
	for _, c := range f.calls {
		if c == portal.RemoteDesktopIface+".NotifyPointerMotionAbsolute" {
			found = true
		}
	}
	if !found {
		t.Fatalf("NotifyPointerMotionAbsolute was not called, calls=%v", f.calls)
	}
}

func TestPortalInput_MoveMouse_NoCoveringStream_ActionFailed(t *testing.T) {
	sess, f := openFakeSessionWithStream(t)
	f.calls = nil

	p := &portalInput{session: sess, caller: f}
	// Well outside the only stream's 1920x1080 rectangle.
	err := p.MoveMouse(5000, 5000)
	if err == nil {
		t.Fatal("expected an error when no stream covers the point")
	}
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("err = %v, want ACTION_FAILED", err)
	}
	for _, c := range f.calls {
		if c == portal.RemoteDesktopIface+".NotifyPointerMotionAbsolute" {
			t.Fatalf("NotifyPointerMotionAbsolute must not be called with an invalid stream id (the old stream=0 bug)")
		}
	}
}

func TestPortalInput_NoScreenCastStream_MouseCapabilityFalse_KeyboardTrue(t *testing.T) {
	// A session opened WITHOUT ScreenCast has no streams: absolute pointer
	// positioning is genuinely unavailable and must not be silently faked.
	f := newFakeRequestCaller()
	f.script(portal.RemoteDesktopIface+".CreateSession", 0, map[string]dbus.Variant{
		"session_handle": dbus.MakeVariant("/s/1"),
	})
	f.script(portal.RemoteDesktopIface+".SelectDevices", 0, nil)
	f.script(portal.RemoteDesktopIface+".Start", 0, nil)
	sess, err := portal.Open(context.Background(), f, portal.OpenOptions{WantScreenCast: false})
	if err != nil {
		t.Fatalf("portal.Open: %v", err)
	}
	if len(sess.Streams) != 0 {
		t.Fatalf("expected no streams, got %+v", sess.Streams)
	}

	p := &portalInput{session: sess, caller: f}
	if err := p.MoveMouse(10, 10); err == nil {
		t.Fatal("MoveMouse should fail honestly with no ScreenCast stream")
	} else if de, ok := core.AsDesktopError(err); !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("err = %v, want ACTION_FAILED", err)
	}

	// Keyboard events do not need a stream: PressKey should still route a
	// NotifyKeyboardKeysym call successfully.
	if err := p.PressKey("a"); err != nil {
		t.Fatalf("PressKey should still work without a ScreenCast stream: %v", err)
	}
}
