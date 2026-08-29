// Package portal implements the shared plumbing for talking to the XDG
// desktop portal (org.freedesktop.portal.Desktop) that internal/uiauto's
// Linux Wayland input (internal/uiauto/input) and screen capture
// (internal/uiauto/screen) packages both need: the well-known bus/object
// names, org.freedesktop.portal.Request handle-token generation and
// Response-signal handling, response-code -> core.DesktopError mapping,
// and consent timeouts.
//
// Before this package existed, input_linux.go and screen_linux.go each
// duplicated this plumbing, which is exactly what made it impossible for
// them to share one RemoteDesktop+ScreenCast session (see the Wayland
// parity plan, block W1/W2): a portalInput.MoveMouse call addressed
// ScreenCast stream id 0, which is never a valid stream absent a real
// ScreenCast session negotiated on the SAME session handle as the pointer
// device selection. This package makes that one-session flow possible; see
// Session/Open in session.go.
//
// No file in this package uses cgo.
package portal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/godbus/dbus/v5"
)

// Well-known XDG desktop portal bus/object/interface names.
const (
	BusName            = "org.freedesktop.portal.Desktop"
	ObjectPath         = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	RequestIface       = "org.freedesktop.portal.Request"
	SessionIface       = "org.freedesktop.portal.Session"
	RemoteDesktopIface = "org.freedesktop.portal.RemoteDesktop"
	ScreenCastIface    = "org.freedesktop.portal.ScreenCast"
	ScreenshotIface    = "org.freedesktop.portal.Screenshot"
)

// RemoteDesktop.SelectDevices DeviceType bit flags.
const (
	DeviceKeyboard uint32 = 1
	DevicePointer  uint32 = 2
)

// ScreenCast.SelectSources SourceType bit flags.
const (
	SourceMonitor uint32 = 1
)

// SelectDevices/SelectSources persist_mode values. PersistUntilRevoked is
// what W3 (consent persistence) needs: without it the compositor forgets
// the grant as soon as the session ends and the user is re-prompted on
// every single Pando run.
const (
	PersistNone         uint32 = 0
	PersistUntilLogout  uint32 = 1
	PersistUntilRevoked uint32 = 2
)

// ConsentTimeout bounds how long a caller waits for the user to respond to
// a portal consent dialog (or for the portal to fail outright when no
// compositor/portal backend is running at all).
const ConsentTimeout = 30 * time.Second

var handleTokenCounter int64

// newHandleToken generates a unique-enough handle/session token for a
// portal request. The XDG portal spec requires each Request's handle_token
// (and each session's session_handle_token) to be unique per sender; a
// timestamp plus a process-local monotonic counter satisfies that without
// needing crypto-strength randomness.
func newHandleToken(prefix string) string {
	n := atomic.AddInt64(&handleTokenCounter, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), n)
}

// NewHandleToken exposes newHandleToken to other packages in this module
// (internal/uiauto/screen's Screenshot portal call also needs a unique
// handle_token per the XDG portal spec, outside of a Session).
func NewHandleToken(prefix string) string { return newHandleToken(prefix) }

// RequestCaller abstracts the D-Bus surface this package's session/request
// logic depends on, so tests can substitute a fake in-memory portal
// instead of a real xdg-desktop-portal backend (none is running on the dev
// box this was built on: no DISPLAY/WAYLAND_DISPLAY, no portal).
type RequestCaller interface {
	// Request invokes path's iface.method(args...), which per the XDG
	// portal convention must return an org.freedesktop.portal.Request
	// object path, and waits (bounded by ctx) for that Request's Response
	// signal, returning its response code and results.
	Request(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) (code uint32, results map[string]dbus.Variant, err error)
	// Call invokes a plain (non-Request) D-Bus method on path and returns
	// its raw reply body.
	Call(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error)
	// Close releases the underlying D-Bus connection.
	Close() error
}

// dbusCaller is the real RequestCaller, backed by a live session-bus
// connection.
type dbusCaller struct {
	conn *dbus.Conn
}

// Dial connects to the D-Bus session bus for the desktop portal. Callers
// own the returned RequestCaller's lifetime and must Close it.
func Dial() (RequestCaller, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	return &dbusCaller{conn: conn}, nil
}

func (d *dbusCaller) Call(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error) {
	obj := d.conn.Object(BusName, path)
	call := obj.CallWithContext(ctx, iface+"."+method, 0, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Body, nil
}

func (d *dbusCaller) Request(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) (uint32, map[string]dbus.Variant, error) {
	obj := d.conn.Object(BusName, path)
	call := obj.CallWithContext(ctx, iface+"."+method, 0, args...)
	if call.Err != nil {
		return 0, nil, call.Err
	}
	var reqPath dbus.ObjectPath
	if err := call.Store(&reqPath); err != nil {
		return 0, nil, err
	}

	ch := make(chan *dbus.Signal, 1)
	d.conn.Signal(ch)
	defer d.conn.RemoveSignal(ch)
	if err := d.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(reqPath),
		dbus.WithMatchInterface(RequestIface),
		dbus.WithMatchMember("Response"),
	); err != nil {
		return 0, nil, err
	}

	for {
		select {
		case <-ctx.Done():
			return 0, nil, core.NewTimeoutError("timed out waiting for the desktop portal response for " + iface + "." + method)
		case sig := <-ch:
			if sig.Path != reqPath || sig.Name != RequestIface+".Response" {
				continue
			}
			if len(sig.Body) < 2 {
				return 0, nil, fmt.Errorf("malformed portal Response signal for %s.%s", iface, method)
			}
			code, _ := sig.Body[0].(uint32)
			results, _ := sig.Body[1].(map[string]dbus.Variant)
			return code, results, nil
		}
	}
}

func (d *dbusCaller) Close() error {
	if d.conn == nil {
		return nil
	}
	return d.conn.Close()
}

// ResponseCodeError maps an org.freedesktop.portal.Request response code to
// a *core.DesktopError. Per the XDG portal spec: 0 is success (nil is
// returned); 1 means the user explicitly cancelled/declined the request,
// mapped to PERM_DENIED so the LLM gets an actionable "the user said no"
// signal rather than a generic failure; any other non-zero code (2 =
// "ended"/other error, or a backend-specific code) is reported as
// ACTION_FAILED, since the portal itself was reachable but the operation
// could not complete.
func ResponseCodeError(step string, code uint32) error {
	switch code {
	case 0:
		return nil
	case 1:
		return core.NewPermDeniedError(fmt.Sprintf("the user cancelled the desktop portal %s request", step))
	default:
		return core.NewActionFailedError(fmt.Sprintf("the desktop portal %s request failed (response code %d)", step, code))
	}
}

// Request performs one Request-pattern portal call and maps its outcome:
// a transport-level error (portal missing, D-Bus unreachable) is reported
// as PLATFORM_NOT_SUPPORTED, a non-zero response code is mapped via
// ResponseCodeError, and success returns the raw results.
func Request(ctx context.Context, caller RequestCaller, path dbus.ObjectPath, iface, method string, args ...interface{}) (map[string]dbus.Variant, error) {
	code, results, err := caller.Request(ctx, path, iface, method, args...)
	if err != nil {
		if de, ok := core.AsDesktopError(err); ok {
			return nil, de
		}
		return nil, core.NewPlatformNotSupportedError(fmt.Sprintf("desktop portal %s.%s failed: %v", iface, method, err))
	}
	if rerr := ResponseCodeError(iface+"."+method, code); rerr != nil {
		return nil, rerr
	}
	return results, nil
}

// current holds the most recently opened Session, shared across the
// input and screen packages so, e.g., a screenshot's Displays() call can
// report real ScreenCast stream geometry when input has already
// negotiated a session, without forcing its own separate consent flow.
var (
	currentMu sync.RWMutex
	current   *Session
)

// SetCurrent registers sess as the process-wide shared portal session.
// Passing nil clears it. Safe for concurrent use.
func SetCurrent(sess *Session) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = sess
}

// Current returns the process-wide shared portal session, or nil if none
// has been established yet.
func Current() *Session {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}
