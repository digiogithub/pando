package portal

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/godbus/dbus/v5"
)

// Stream is one ScreenCast stream from a Start response: a PipeWire node
// id plus the position/size rectangle it covers in the compositor's global
// (absolute pointer) coordinate space. This rectangle is exactly the
// coordinate space org.freedesktop.portal.RemoteDesktop.
// NotifyPointerMotionAbsolute needs: the portal has no meaning for
// "absolute (x,y)" without a stream saying which capture region those
// coordinates are relative to.
type Stream struct {
	NodeID uint32
	X, Y   int
	W, H   int
}

// Contains reports whether the global point (x,y) falls within the
// stream's rectangle.
func (s Stream) Contains(x, y int) bool {
	return x >= s.X && x < s.X+s.W && y >= s.Y && y < s.Y+s.H
}

// Session is one combined RemoteDesktop+ScreenCast portal session: a
// single session_handle produced by RemoteDesktop.CreateSession, against
// which SelectDevices (keyboard/pointer) and, when requested,
// ScreenCast.SelectSources were both issued before Start. This is W1/W2 of
// the Wayland parity plan: only a session negotiated this way has a
// coordinate space (Streams) that NotifyPointerMotionAbsolute can
// meaningfully address.
type Session struct {
	caller  RequestCaller
	handle  dbus.ObjectPath
	Streams []Stream

	// RemoteDesktopRestoreToken/ScreenCastRestoreToken are the restore
	// tokens the portal returned from this Start call, when persist_mode
	// was honoured. Empty when the compositor does not support restore
	// tokens or none was granted.
	RemoteDesktopRestoreToken string
	ScreenCastRestoreToken    string
}

// Handle returns the session_handle object path, the first argument every
// RemoteDesktop/ScreenCast notify/select call needs.
func (s *Session) Handle() dbus.ObjectPath { return s.handle }

// StreamFor returns the Stream whose rectangle contains the global point
// (x,y), if any.
func (s *Session) StreamFor(x, y int) (Stream, bool) {
	for _, st := range s.Streams {
		if st.Contains(x, y) {
			return st, true
		}
	}
	return Stream{}, false
}

// Close releases the session's D-Bus objects: it asks the portal to close
// the session (best effort — the portal may already have torn it down)
// and then closes the underlying connection. It does not clear the
// process-wide Current() session; callers that set it should also clear it
// (SetCurrent(nil)) when appropriate.
func (s *Session) Close() {
	if s == nil || s.caller == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.caller.Call(ctx, s.handle, SessionIface, "Close")
	_ = s.caller.Close()
}

// OpenOptions configures Open.
type OpenOptions struct {
	// WantScreenCast, when true, also negotiates ScreenCast.SelectSources
	// on the same session handle, which is what gives
	// NotifyPointerMotionAbsolute a coordinate space (Session.Streams).
	// When false, only RemoteDesktop devices are selected: relative
	// pointer motion and keyboard events work, but absolute positioning
	// does not (there is no stream to address).
	WantScreenCast bool

	// RemoteDesktopRestoreToken/ScreenCastRestoreToken, when non-empty,
	// are passed back to the portal so a previously-granted consent can be
	// restored without a fresh prompt (W3). A rejected/expired token
	// degrades to a fresh consent prompt automatically: see Open.
	RemoteDesktopRestoreToken string
	ScreenCastRestoreToken    string

	// ConsentTimeout overrides ConsentTimeout when non-zero.
	ConsentTimeout time.Duration
}

// Open establishes ONE combined RemoteDesktop(+ScreenCast) portal session:
// CreateSession, then SelectDevices and (when WantScreenCast) SelectSources
// against that SAME session handle, then Start. This is the fix for the
// pre-existing bug where MoveMouse addressed a hardcoded, nonexistent
// stream id 0: only a session negotiated this way has a real coordinate
// space to address.
//
// When a restore token was supplied and the attempt using it fails, Open
// retries once with a fresh (tokenless) session rather than returning a
// hard failure — a rejected or expired restore token must degrade to a new
// consent prompt, not break the feature outright.
func Open(ctx context.Context, caller RequestCaller, opts OpenOptions) (*Session, error) {
	sess, err := open(ctx, caller, opts)
	if err != nil && (opts.RemoteDesktopRestoreToken != "" || opts.ScreenCastRestoreToken != "") {
		fresh := opts
		fresh.RemoteDesktopRestoreToken = ""
		fresh.ScreenCastRestoreToken = ""
		return open(ctx, caller, fresh)
	}
	return sess, err
}

func open(ctx context.Context, caller RequestCaller, opts OpenOptions) (*Session, error) {
	timeout := opts.ConsentTimeout
	if timeout <= 0 {
		timeout = ConsentTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	createResults, err := Request(cctx, caller, ObjectPath, RemoteDesktopIface, "CreateSession", map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant(newHandleToken("pandosession")),
	})
	if err != nil {
		return nil, err
	}
	sessionHandleStr, _ := createResults["session_handle"].Value().(string)
	if sessionHandleStr == "" {
		return nil, core.NewPermDeniedError("the desktop portal did not return a session handle from CreateSession")
	}
	sess := &Session{caller: caller, handle: dbus.ObjectPath(sessionHandleStr)}

	devOpts := map[string]dbus.Variant{
		"types":        dbus.MakeVariant(DeviceKeyboard | DevicePointer),
		"persist_mode": dbus.MakeVariant(PersistUntilRevoked),
		"handle_token": dbus.MakeVariant(newHandleToken("pandoreq")),
	}
	if opts.RemoteDesktopRestoreToken != "" {
		devOpts["restore_token"] = dbus.MakeVariant(opts.RemoteDesktopRestoreToken)
	}
	if _, err := Request(cctx, caller, ObjectPath, RemoteDesktopIface, "SelectDevices", sess.handle, devOpts); err != nil {
		return nil, err
	}

	if opts.WantScreenCast {
		srcOpts := map[string]dbus.Variant{
			"types":        dbus.MakeVariant(SourceMonitor),
			"persist_mode": dbus.MakeVariant(PersistUntilRevoked),
			"handle_token": dbus.MakeVariant(newHandleToken("pandoreq")),
		}
		if opts.ScreenCastRestoreToken != "" {
			srcOpts["restore_token"] = dbus.MakeVariant(opts.ScreenCastRestoreToken)
		}
		if _, err := Request(cctx, caller, ObjectPath, ScreenCastIface, "SelectSources", sess.handle, srcOpts); err != nil {
			return nil, err
		}
	}

	startResults, err := Request(cctx, caller, ObjectPath, RemoteDesktopIface, "Start", sess.handle, "", map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(newHandleToken("pandoreq")),
	})
	if err != nil {
		return nil, err
	}

	if v, ok := startResults["restore_token"]; ok {
		if tok, ok := v.Value().(string); ok && tok != "" {
			if opts.WantScreenCast {
				sess.ScreenCastRestoreToken = tok
			} else {
				sess.RemoteDesktopRestoreToken = tok
			}
		}
	}
	if v, ok := startResults["streams"]; ok {
		streams, derr := decodeStreams(v.Value())
		if derr == nil {
			sess.Streams = streams
		}
		// A malformed streams payload is not fatal to the session itself
		// (keyboard/relative-ish functionality can still work); it simply
		// leaves Streams empty, which callers must treat as "no absolute
		// positioning available" per W2.
	}

	return sess, nil
}

// decodeStreams decodes the "streams" entry of a RemoteDesktop/ScreenCast
// Start response, wire type a(ua{sv}): an array of (node_id, properties)
// pairs where properties carries "position" (ii) and "size" (ii). godbus
// decodes each array element generically as []interface{}, so this walks
// the reflected slice rather than relying on dbus.Store (which cannot
// decode array-of-struct into a typed Go slice; see the equivalent
// storeSoRefSlice helper in internal/uiauto/platform/linux for the same
// constraint).
func decodeStreams(raw interface{}) ([]Stream, error) {
	rv := reflect.ValueOf(raw)
	if rv.Kind() != reflect.Slice {
		return nil, fmt.Errorf("portal: expected an array for streams, got %T", raw)
	}
	out := make([]Stream, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		fields, ok := elem.([]interface{})
		if !ok || len(fields) != 2 {
			return nil, fmt.Errorf("portal: expected stream element %d to be a 2-field (u,a{sv}) struct, got %T", i, elem)
		}
		nodeID, err := toUint32(fields[0])
		if err != nil {
			return nil, fmt.Errorf("portal: stream element %d node id: %w", i, err)
		}
		props, ok := fields[1].(map[string]dbus.Variant)
		if !ok {
			return nil, fmt.Errorf("portal: stream element %d properties: expected map[string]dbus.Variant, got %T", i, fields[1])
		}
		st := Stream{NodeID: nodeID}
		if pos, ok := decodeIntPair(props["position"]); ok {
			st.X, st.Y = pos[0], pos[1]
		}
		if size, ok := decodeIntPair(props["size"]); ok {
			st.W, st.H = size[0], size[1]
		}
		out = append(out, st)
	}
	return out, nil
}

// decodeIntPair decodes a "(ii)" struct variant (godbus: []interface{} of
// two integers) into a [2]int.
func decodeIntPair(v dbus.Variant) ([2]int, bool) {
	raw := v.Value()
	if raw == nil {
		return [2]int{}, false
	}
	fields, ok := raw.([]interface{})
	if !ok || len(fields) != 2 {
		return [2]int{}, false
	}
	a, aerr := toInt(fields[0])
	b, berr := toInt(fields[1])
	if aerr != nil || berr != nil {
		return [2]int{}, false
	}
	return [2]int{a, b}, true
}

func toInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int16:
		return int(n), nil
	case uint16:
		return int(n), nil
	case int32:
		return int(n), nil
	case uint32:
		return int(n), nil
	case int64:
		return int(n), nil
	case uint64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, errors.New("unsupported integer wire type")
	}
}

func toUint32(v interface{}) (uint32, error) {
	n, err := toInt(v)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}
