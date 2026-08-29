package portal

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/godbus/dbus/v5"
)

// fakeCall records one Request/Call invocation and lets tests script a
// canned response, so Open()/Session methods can be exercised without a
// real xdg-desktop-portal backend (none is available in this dev
// environment: no DISPLAY/WAYLAND_DISPLAY, no portal running).
type fakeResponse struct {
	code    uint32
	results map[string]dbus.Variant
	err     error
}

type fakeCaller struct {
	// requests maps "iface.method" -> the sequence of responses to return,
	// consumed in order across repeated calls to that key.
	requests map[string][]fakeResponse
	calls    []string // records "iface.method" call order
	closed   bool
}

func newFakeCaller() *fakeCaller {
	return &fakeCaller{requests: make(map[string][]fakeResponse)}
}

func (f *fakeCaller) script(key string, resp fakeResponse) {
	f.requests[key] = append(f.requests[key], resp)
}

func (f *fakeCaller) Request(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) (uint32, map[string]dbus.Variant, error) {
	key := iface + "." + method
	f.calls = append(f.calls, key)
	queue := f.requests[key]
	if len(queue) == 0 {
		return 0, nil, nil // default: success, empty results
	}
	resp := queue[0]
	f.requests[key] = queue[1:]
	return resp.code, resp.results, resp.err
}

func (f *fakeCaller) Call(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error) {
	f.calls = append(f.calls, iface+"."+method)
	return nil, nil
}

func (f *fakeCaller) Close() error {
	f.closed = true
	return nil
}

// streamsVariant builds a "streams" a(ua{sv}) reply value for nodeID with
// the given position/size, in the shape godbus decodes it to.
func streamsVariant(entries ...struct {
	nodeID     uint32
	x, y, w, h int
}) []interface{} {
	out := make([]interface{}, 0, len(entries))
	for _, e := range entries {
		props := map[string]dbus.Variant{
			"position": dbus.MakeVariant([]interface{}{int32(e.x), int32(e.y)}),
			"size":     dbus.MakeVariant([]interface{}{int32(e.w), int32(e.h)}),
		}
		out = append(out, []interface{}{e.nodeID, props})
	}
	return out
}

func withSessionHandle(handle string) fakeResponse {
	return fakeResponse{code: 0, results: map[string]dbus.Variant{
		"session_handle": dbus.MakeVariant(handle),
	}}
}

func TestOpen_HappyPath_ScreenCastStreams(t *testing.T) {
	f := newFakeCaller()
	f.script(RemoteDesktopIface+".CreateSession", withSessionHandle("/org/freedesktop/portal/desktop/session/1"))
	f.script(RemoteDesktopIface+".SelectDevices", fakeResponse{code: 0})
	f.script(ScreenCastIface+".SelectSources", fakeResponse{code: 0})
	f.script(RemoteDesktopIface+".Start", fakeResponse{code: 0, results: map[string]dbus.Variant{
		"restore_token": dbus.MakeVariant("tok-abc"),
		"streams": dbus.MakeVariant(streamsVariant(struct {
			nodeID     uint32
			x, y, w, h int
		}{nodeID: 7, x: 0, y: 0, w: 1920, h: 1080})),
	}})

	sess, err := Open(context.Background(), f, OpenOptions{WantScreenCast: true})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if sess.Handle() != "/org/freedesktop/portal/desktop/session/1" {
		t.Errorf("handle = %q", sess.Handle())
	}
	if len(sess.Streams) != 1 || sess.Streams[0].NodeID != 7 {
		t.Fatalf("Streams = %+v", sess.Streams)
	}
	if sess.ScreenCastRestoreToken != "tok-abc" {
		t.Errorf("ScreenCastRestoreToken = %q, want tok-abc", sess.ScreenCastRestoreToken)
	}

	stream, ok := sess.StreamFor(500, 500)
	if !ok || stream.NodeID != 7 {
		t.Fatalf("StreamFor(500,500) = %+v, %v", stream, ok)
	}
	if _, ok := sess.StreamFor(3000, 3000); ok {
		t.Errorf("StreamFor(3000,3000) unexpectedly matched a stream")
	}

	wantCalls := []string{
		RemoteDesktopIface + ".CreateSession",
		RemoteDesktopIface + ".SelectDevices",
		ScreenCastIface + ".SelectSources",
		RemoteDesktopIface + ".Start",
	}
	if len(f.calls) != len(wantCalls) {
		t.Fatalf("calls = %v, want %v", f.calls, wantCalls)
	}
	for i, c := range wantCalls {
		if f.calls[i] != c {
			t.Errorf("call[%d] = %q, want %q", i, f.calls[i], c)
		}
	}
}

func TestOpen_NoScreenCast_NoStreams(t *testing.T) {
	f := newFakeCaller()
	f.script(RemoteDesktopIface+".CreateSession", withSessionHandle("/s/1"))
	f.script(RemoteDesktopIface+".SelectDevices", fakeResponse{code: 0})
	f.script(RemoteDesktopIface+".Start", fakeResponse{code: 0})

	sess, err := Open(context.Background(), f, OpenOptions{WantScreenCast: false})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if len(sess.Streams) != 0 {
		t.Errorf("Streams = %+v, want none (no ScreenCast negotiated)", sess.Streams)
	}
	for _, c := range f.calls {
		if c == ScreenCastIface+".SelectSources" {
			t.Errorf("SelectSources was called even though WantScreenCast was false")
		}
	}
}

func TestOpen_UserCancelled_MapsToPermDenied(t *testing.T) {
	f := newFakeCaller()
	f.script(RemoteDesktopIface+".CreateSession", withSessionHandle("/s/1"))
	f.script(RemoteDesktopIface+".SelectDevices", fakeResponse{code: 1}) // user cancelled

	_, err := Open(context.Background(), f, OpenOptions{})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPermDenied {
		t.Fatalf("err = %v, want PERM_DENIED", err)
	}
}

func TestOpen_RestoreTokenRejected_FallsBackToFreshConsent(t *testing.T) {
	f := newFakeCaller()
	// First attempt (with restore token) fails at SelectDevices, as if the
	// compositor rejected/expired the token.
	f.script(RemoteDesktopIface+".CreateSession", withSessionHandle("/s/1"))
	f.script(RemoteDesktopIface+".SelectDevices", fakeResponse{code: 1})
	// Retry (fresh, tokenless) succeeds.
	f.script(RemoteDesktopIface+".CreateSession", withSessionHandle("/s/2"))
	f.script(RemoteDesktopIface+".SelectDevices", fakeResponse{code: 0})
	f.script(RemoteDesktopIface+".Start", fakeResponse{code: 0})

	sess, err := Open(context.Background(), f, OpenOptions{RemoteDesktopRestoreToken: "stale-token"})
	if err != nil {
		t.Fatalf("Open with expired token should degrade to a fresh prompt, got error: %v", err)
	}
	if sess.Handle() != "/s/2" {
		t.Errorf("handle = %q, want the fresh-session handle /s/2", sess.Handle())
	}

	// CreateSession must have been attempted twice (once per session).
	createCount := 0
	for _, c := range f.calls {
		if c == RemoteDesktopIface+".CreateSession" {
			createCount++
		}
	}
	if createCount != 2 {
		t.Errorf("CreateSession called %d times, want 2 (initial + fallback)", createCount)
	}
}

func TestOpen_NoTokenAndFailure_DoesNotRetry(t *testing.T) {
	f := newFakeCaller()
	f.script(RemoteDesktopIface+".CreateSession", withSessionHandle("/s/1"))
	f.script(RemoteDesktopIface+".SelectDevices", fakeResponse{code: 1})

	_, err := Open(context.Background(), f, OpenOptions{}) // no restore tokens supplied
	if err == nil {
		t.Fatalf("expected an error")
	}
	createCount := 0
	for _, c := range f.calls {
		if c == RemoteDesktopIface+".CreateSession" {
			createCount++
		}
	}
	if createCount != 1 {
		t.Errorf("CreateSession called %d times, want exactly 1 (no retry without a restore token)", createCount)
	}
}

func TestOpen_TransportError_MapsToPlatformNotSupported(t *testing.T) {
	f := newFakeCaller()
	f.script(RemoteDesktopIface+".CreateSession", fakeResponse{err: errUnreachable})

	_, err := Open(context.Background(), f, OpenOptions{})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("err = %v, want PLATFORM_NOT_SUPPORTED", err)
	}
}

func TestOpen_NoSessionHandle_PermDenied(t *testing.T) {
	f := newFakeCaller()
	f.script(RemoteDesktopIface+".CreateSession", fakeResponse{code: 0}) // no session_handle key

	_, err := Open(context.Background(), f, OpenOptions{})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPermDenied {
		t.Fatalf("err = %v, want PERM_DENIED", err)
	}
}

func TestResponseCodeError(t *testing.T) {
	if err := ResponseCodeError("X", 0); err != nil {
		t.Errorf("code 0 should be nil, got %v", err)
	}
	de, ok := core.AsDesktopError(ResponseCodeError("X", 1))
	if !ok || de.Code != core.ErrPermDenied {
		t.Errorf("code 1 should be PERM_DENIED, got %v", de)
	}
	de, ok = core.AsDesktopError(ResponseCodeError("X", 2))
	if !ok || de.Code != core.ErrActionFailed {
		t.Errorf("code 2 should be ACTION_FAILED, got %v", de)
	}
	de, ok = core.AsDesktopError(ResponseCodeError("X", 99))
	if !ok || de.Code != core.ErrActionFailed {
		t.Errorf("unknown non-zero code should be ACTION_FAILED, got %v", de)
	}
}

func TestDecodeStreams_MultipleAndMalformed(t *testing.T) {
	raw := streamsVariant(
		struct {
			nodeID     uint32
			x, y, w, h int
		}{nodeID: 1, x: 0, y: 0, w: 1920, h: 1080},
		struct {
			nodeID     uint32
			x, y, w, h int
		}{nodeID: 2, x: 1920, y: 0, w: 1280, h: 720},
	)
	streams, err := decodeStreams(raw)
	if err != nil {
		t.Fatalf("decodeStreams: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("streams = %+v", streams)
	}
	if streams[1].X != 1920 || streams[1].W != 1280 {
		t.Errorf("second stream = %+v", streams[1])
	}

	if _, err := decodeStreams("not a slice"); err == nil {
		t.Errorf("expected an error for a non-slice input")
	}
	if _, err := decodeStreams([]interface{}{"not a struct"}); err == nil {
		t.Errorf("expected an error for a malformed element")
	}
}

func TestSessionCurrent(t *testing.T) {
	SetCurrent(nil)
	if Current() != nil {
		t.Fatalf("expected no current session")
	}
	sess := &Session{handle: "/s/1"}
	SetCurrent(sess)
	if Current() != sess {
		t.Fatalf("Current() did not return the session that was set")
	}
	SetCurrent(nil)
	if Current() != nil {
		t.Fatalf("expected Current() to be cleared")
	}
}

// errUnreachable is a plain (non-DesktopError) transport-level error, as a
// real dbus.ConnectSessionBus/CallWithContext failure would be.
var errUnreachable = fakeTransportErr("no portal backend is running")

type fakeTransportErr string

func (e fakeTransportErr) Error() string { return string(e) }
