//go:build linux

package input

import (
	"context"
	"fmt"
	"os"
	"sync"
	"unicode/utf16"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/portal"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// This file implements core.PhysicalInput on Linux. On X11 (or XWayland),
// synthetic input is sent via the XTEST extension (github.com/jezek/xgb),
// a pure-Go X11 client with no cgo. On a pure Wayland session (no X11
// available), the XDG desktop portal RemoteDesktop interface is used
// instead, over godbus; that path requires an interactive user consent
// dialog from the portal backend (xdg-desktop-portal), and honestly
// reports PERM_DENIED when that consent cannot be obtained rather than
// silently no-oping.

// sessionType reports which display protocol is in play, mirroring the
// detection rules in the Phase 3 plan: WAYLAND_DISPLAY set means Wayland;
// otherwise DISPLAY set means X11 (including XWayland); neither means no
// display session at all.
func sessionType() string {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if os.Getenv("DISPLAY") != "" {
			// XWayland is present: prefer the X11 XTEST path, it's
			// synchronous and needs no consent dialog.
			return "x11"
		}
		return "wayland"
	}
	if os.Getenv("DISPLAY") != "" {
		return "x11"
	}
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return "wayland"
	}
	return "none"
}

// New constructs the Linux PhysicalInput implementation appropriate for
// the current session (X11 XTEST, or the Wayland RemoteDesktop portal). It
// never fails to construct; per-session unavailability surfaces as
// PLATFORM_NOT_SUPPORTED/PERM_DENIED from individual method calls, so a
// Manager can always be built and simply report reduced Capabilities.
func New() (core.PhysicalInput, error) {
	switch sessionType() {
	case "x11":
		return &x11Input{}, nil
	case "wayland":
		return &portalInput{}, nil
	default:
		return noSessionInput{}, nil
	}
}

// Capabilities probes what this platform's physical input implementation
// can actually do in the current session.
func Capabilities() core.Capabilities {
	switch sessionType() {
	case "x11":
		x := &x11Input{}
		if err := x.connect(); err != nil {
			return core.Capabilities{}
		}
		defer x.close()
		return core.Capabilities{Mouse: true, Keyboard: true}
	case "wayland":
		p := &portalInput{}
		defer p.closeSession()
		if err := p.ensureSession(); err != nil {
			return core.Capabilities{}
		}
		// Keyboard/relative-ish input works as soon as the RemoteDesktop
		// session is started; absolute pointer positioning additionally
		// needs a real ScreenCast stream to address (W2) — without one,
		// MoveMouse/Click/Scroll (the only pointer operations this
		// interface exposes) cannot be offered honestly.
		return core.Capabilities{Mouse: len(p.session.Streams) > 0, Keyboard: true}
	default:
		return core.Capabilities{}
	}
}

// noSessionInput is used when neither DISPLAY nor WAYLAND_DISPLAY is set
// (e.g. a plain tty/headless session, such as this development box).
type noSessionInput struct{}

func (noSessionInput) Click(x, y int) error {
	return core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
}
func (noSessionInput) MoveMouse(x, y int) error {
	return core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
}
func (noSessionInput) TypeText(s string) error {
	return core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
}
func (noSessionInput) PressKey(key string) error {
	return core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
}
func (noSessionInput) Scroll(x, y, amount int) error {
	return core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
}

// ---- X11 (XTEST) ----

const (
	x11KeyPress      = 2
	x11KeyRelease    = 3
	x11ButtonPress   = 4
	x11ButtonRelease = 5
	x11MotionNotify  = 6

	x11ButtonLeft     = 1
	x11ButtonScrollUp = 4
	x11ButtonScrollDn = 5
)

// namedKeysyms maps the canonical named-key vocabulary (see keys.go) to
// standard X11 keysymdef.h values.
var namedKeysyms = map[string]uint32{
	"enter": 0xff0d, "tab": 0xff09, "escape": 0xff1b, "space": 0x0020,
	"backspace": 0xff08, "delete": 0xffff,
	"up": 0xff52, "down": 0xff54, "left": 0xff51, "right": 0xff53,
	"home": 0xff50, "end": 0xff57, "pageup": 0xff55, "pagedown": 0xff56,
	"insert": 0xff63, "capslock": 0xffe5,
	"f1": 0xffbe, "f2": 0xffbf, "f3": 0xffc0, "f4": 0xffc1,
	"f5": 0xffc2, "f6": 0xffc3, "f7": 0xffc4, "f8": 0xffc5,
	"f9": 0xffc6, "f10": 0xffc7, "f11": 0xffc8, "f12": 0xffc9,
}

const (
	xkControlL = 0xffe3
	xkAltL     = 0xffe9
	xkShiftL   = 0xffe1
	xkSuperL   = 0xffeb
)

// runeToKeysym converts a Unicode code point to its X11 keysym using the
// standard Unicode-keysym convention: Latin-1 code points (0x20-0xff) are
// their own keysym; everything else is 0x01000000 | codepoint.
func runeToKeysym(r rune) uint32 {
	if r >= 0x20 && r <= 0xff {
		return uint32(r)
	}
	return 0x01000000 | uint32(r)
}

// x11Input implements core.PhysicalInput via the XTEST extension. Each
// call opens and closes its own connection: synthetic input is rare
// enough (agent-driven, not a hot loop) that this is simpler and safer
// than managing a shared long-lived connection's lifecycle across
// concurrent tool calls.
type x11Input struct {
	mu   sync.Mutex
	conn *xgb.Conn
}

func (x *x11Input) connect() error {
	conn, err := xgb.NewConn()
	if err != nil {
		return core.NewPlatformNotSupportedError("could not connect to the X11 display: " + err.Error())
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return core.NewPlatformNotSupportedError("XTEST extension is not available on this X server: " + err.Error())
	}
	x.conn = conn
	return nil
}

func (x *x11Input) close() {
	if x.conn != nil {
		x.conn.Close()
		x.conn = nil
	}
}

func (x *x11Input) root() xproto.Window {
	setup := xproto.Setup(x.conn)
	return setup.DefaultScreen(x.conn).Root
}

func (x *x11Input) fake(typ byte, detail byte, rootX, rootY int16) error {
	return xtest.FakeInputChecked(x.conn, typ, detail, 0, x.root(), rootX, rootY, 0).Check()
}

func (x *x11Input) MoveMouse(px, py int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.connect(); err != nil {
		return err
	}
	defer x.close()
	if err := x.fake(x11MotionNotify, 0, int16(px), int16(py)); err != nil {
		return core.NewActionFailedError("XTestFakeInput motion failed: " + err.Error())
	}
	return nil
}

func (x *x11Input) Click(px, py int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.connect(); err != nil {
		return err
	}
	defer x.close()
	if err := x.fake(x11MotionNotify, 0, int16(px), int16(py)); err != nil {
		return core.NewActionFailedError("XTestFakeInput motion failed: " + err.Error())
	}
	if err := x.fake(x11ButtonPress, x11ButtonLeft, 0, 0); err != nil {
		return core.NewActionFailedError("XTestFakeInput button press failed: " + err.Error())
	}
	if err := x.fake(x11ButtonRelease, x11ButtonLeft, 0, 0); err != nil {
		return core.NewActionFailedError("XTestFakeInput button release failed: " + err.Error())
	}
	return nil
}

func (x *x11Input) Scroll(px, py, amount int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.connect(); err != nil {
		return err
	}
	defer x.close()
	if err := x.fake(x11MotionNotify, 0, int16(px), int16(py)); err != nil {
		return core.NewActionFailedError("XTestFakeInput motion failed: " + err.Error())
	}
	button := byte(x11ButtonScrollUp)
	n := amount
	if amount < 0 {
		button = x11ButtonScrollDn
		n = -amount
	}
	for i := 0; i < n; i++ {
		if err := x.fake(x11ButtonPress, button, 0, 0); err != nil {
			return core.NewActionFailedError("XTestFakeInput scroll press failed: " + err.Error())
		}
		if err := x.fake(x11ButtonRelease, button, 0, 0); err != nil {
			return core.NewActionFailedError("XTestFakeInput scroll release failed: " + err.Error())
		}
	}
	return nil
}

// sendKeysym presses and releases keysym using a temporary remap of the
// highest available keycode (a common, well-tested trick, also used by
// tools like xdotool, since XTestFakeInput only accepts a keycode, not a
// keysym directly). The original mapping of that keycode is restored
// afterwards.
func (x *x11Input) sendKeysym(keysym uint32) error {
	setup := xproto.Setup(x.conn)
	scratch := setup.MaxKeycode

	orig, err := xproto.GetKeyboardMapping(x.conn, scratch, 1).Reply()
	if err != nil {
		return core.NewActionFailedError("GetKeyboardMapping failed: " + err.Error())
	}
	perKeycode := orig.KeysymsPerKeycode
	if perKeycode == 0 {
		perKeycode = 1
	}
	origSyms := append([]xproto.Keysym(nil), orig.Keysyms...)

	newSyms := make([]xproto.Keysym, perKeycode)
	for i := range newSyms {
		newSyms[i] = xproto.Keysym(keysym)
	}
	if err := xproto.ChangeKeyboardMappingChecked(x.conn, 1, scratch, perKeycode, newSyms).Check(); err != nil {
		return core.NewActionFailedError("ChangeKeyboardMapping failed: " + err.Error())
	}
	// Give the X server a moment to propagate the mapping change before
	// XTEST synthesizes the key events against it.
	x.conn.Sync()

	restore := func() {
		_ = xproto.ChangeKeyboardMappingChecked(x.conn, 1, scratch, perKeycode, origSyms).Check()
	}

	if err := x.fake(x11KeyPress, byte(scratch), 0, 0); err != nil {
		restore()
		return core.NewActionFailedError("XTestFakeInput key press failed: " + err.Error())
	}
	if err := x.fake(x11KeyRelease, byte(scratch), 0, 0); err != nil {
		restore()
		return core.NewActionFailedError("XTestFakeInput key release failed: " + err.Error())
	}
	restore()
	return nil
}

// keycodeForKeysym scans the full keycode range for one whose primary
// keysym matches, so modifier chords use the keyboard's real Control_L /
// Alt_L / Shift_L / Super_L keycodes instead of hardcoded assumptions.
func (x *x11Input) keycodeForKeysym(keysym uint32) (byte, bool) {
	setup := xproto.Setup(x.conn)
	min, max := setup.MinKeycode, setup.MaxKeycode
	count := byte(max - min + 1)
	reply, err := xproto.GetKeyboardMapping(x.conn, min, count).Reply()
	if err != nil || reply.KeysymsPerKeycode == 0 {
		return 0, false
	}
	per := int(reply.KeysymsPerKeycode)
	for i := 0; i < int(count); i++ {
		base := i * per
		if base >= len(reply.Keysyms) {
			break
		}
		if uint32(reply.Keysyms[base]) == keysym {
			return byte(int(min) + i), true
		}
	}
	return 0, false
}

func (x *x11Input) TypeText(s string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.connect(); err != nil {
		return err
	}
	defer x.close()
	for _, r := range s {
		if err := x.sendKeysym(runeToKeysym(r)); err != nil {
			return err
		}
	}
	return nil
}

func (x *x11Input) PressKey(key string) error {
	chord, err := ParseChord(key)
	if err != nil {
		return err
	}

	x.mu.Lock()
	defer x.mu.Unlock()
	if cerr := x.connect(); cerr != nil {
		return cerr
	}
	defer x.close()

	var modKeysyms []uint32
	if chord.HasModifier(ModCtrl) {
		modKeysyms = append(modKeysyms, xkControlL)
	}
	if chord.HasModifier(ModAlt) {
		modKeysyms = append(modKeysyms, xkAltL)
	}
	if chord.HasModifier(ModShift) {
		modKeysyms = append(modKeysyms, xkShiftL)
	}
	if chord.HasModifier(ModCmd) {
		modKeysyms = append(modKeysyms, xkSuperL)
	}

	var modKeycodes []byte
	for _, ks := range modKeysyms {
		kc, ok := x.keycodeForKeysym(ks)
		if !ok {
			return core.NewActionFailedError(fmt.Sprintf("could not resolve a keycode for modifier keysym 0x%x on this keyboard layout", ks))
		}
		modKeycodes = append(modKeycodes, kc)
	}

	var keysym uint32
	if named, ok := namedKeysyms[chord.Key]; ok {
		keysym = named
	} else {
		runes := []rune(chord.Key)
		if len(runes) != 1 {
			return core.NewInvalidArgsError("unrecognized key name " + chord.Key)
		}
		keysym = runeToKeysym(runes[0])
	}

	for _, kc := range modKeycodes {
		if err := x.fake(x11KeyPress, kc, 0, 0); err != nil {
			return core.NewActionFailedError("XTestFakeInput modifier press failed: " + err.Error())
		}
	}
	sendErr := x.sendKeysym(keysym)
	for i := len(modKeycodes) - 1; i >= 0; i-- {
		_ = x.fake(x11KeyRelease, modKeycodes[i], 0, 0)
	}
	return sendErr
}

// ---- Wayland (XDG desktop portal RemoteDesktop + ScreenCast, shared session) ----

// portalInput implements core.PhysicalInput via a single, shared
// RemoteDesktop(+ScreenCast) portal session (internal/uiauto/portal).
// Establishing a session requires the portal backend to show the user an
// interactive consent dialog (Start); when that consent cannot be obtained
// (no portal running, user declines, or this process has no way to
// present the dialog — e.g. a headless/tty session), every method reports
// PERM_DENIED/PLATFORM_NOT_SUPPORTED with an actionable suggestion rather
// than silently doing nothing.
//
// Correctness fix (W1/W2 of the Wayland parity plan): a prior version of
// this file called NotifyPointerMotionAbsolute with a hardcoded stream id
// of 0, which is never a valid ScreenCast stream node id without an actual
// ScreenCast session — the call was rejected or silently mispositioned.
// portalInput now negotiates ScreenCast.SelectSources on the SAME session
// handle as the RemoteDesktop device selection (portal.Open with
// WantScreenCast:true), and MoveMouse/Click/Scroll resolve the target
// point against that session's real Streams (position+size) to find the
// owning stream's node id before calling NotifyPointerMotionAbsolute. When
// no stream covers the point (consent for screen-cast was refused, or the
// portal/compositor is too old to support it), absolute pointing reports
// ACTION_FAILED rather than guessing a coordinate space that does not
// exist; see Capabilities() above, which reports Mouse:false in that case.
type portalInput struct {
	mu      sync.Mutex
	caller  portal.RequestCaller
	session *portal.Session
}

// ensureSession lazily creates and starts the shared portal session. It is
// idempotent: a session is created and started at most once per
// portalInput instance. A previously persisted restore token (W3) is tried
// first so the user is not re-prompted for consent on every run; a
// rejected/expired token is handled transparently by portal.Open, which
// degrades to a fresh consent prompt.
func (p *portalInput) ensureSession() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != nil {
		return nil
	}

	caller, err := portal.Dial()
	if err != nil {
		return core.NewPlatformNotSupportedError("could not connect to the D-Bus session bus for the desktop portal: " + err.Error())
	}

	remoteToken, screenToken := portal.LoadRestoreTokens()
	sess, err := portal.Open(context.Background(), caller, portal.OpenOptions{
		WantScreenCast:            true,
		RemoteDesktopRestoreToken: remoteToken,
		ScreenCastRestoreToken:    screenToken,
	})
	if err != nil {
		_ = caller.Close()
		return err
	}

	if sess.RemoteDesktopRestoreToken != "" || sess.ScreenCastRestoreToken != "" {
		// Best effort: failing to persist just means the next run
		// re-prompts for consent, not a hard failure of this session.
		_ = portal.SaveRestoreTokens(sess.RemoteDesktopRestoreToken, sess.ScreenCastRestoreToken)
	}

	p.caller = caller
	p.session = sess
	portal.SetCurrent(sess)
	return nil
}

// closeSession releases this instance's portal session, if any. Used by
// the package-level Capabilities() probe so a mere capability check does
// not leak a live D-Bus connection.
func (p *portalInput) closeSession() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != nil {
		p.session.Close()
		if portal.Current() == p.session {
			portal.SetCurrent(nil)
		}
		p.session = nil
	}
	p.caller = nil
}

// call invokes a plain (non-Request) RemoteDesktop notify method against
// the shared session handle.
func (p *portalInput) call(member string, args ...interface{}) error {
	if err := p.ensureSession(); err != nil {
		return err
	}
	p.mu.Lock()
	caller, sess := p.caller, p.session
	p.mu.Unlock()

	full := append([]interface{}{sess.Handle()}, args...)
	if _, err := caller.Call(context.Background(), portal.ObjectPath, portal.RemoteDesktopIface, member, full...); err != nil {
		return core.NewActionFailedError("desktop portal " + member + " failed: " + err.Error())
	}
	return nil
}

func (p *portalInput) MoveMouse(x, y int) error {
	if err := p.ensureSession(); err != nil {
		return err
	}
	p.mu.Lock()
	sess := p.session
	p.mu.Unlock()

	stream, ok := sess.StreamFor(x, y)
	if !ok {
		return core.NewActionFailedError(fmt.Sprintf(
			"Wayland absolute pointer positioning to (%d,%d) has no covering screen-cast stream; grant screen-share consent to Pando (or re-run once you can respond to the portal's consent dialog) so absolute pointing has a coordinate space to address",
			x, y,
		))
	}
	return p.call("NotifyPointerMotionAbsolute", stream.NodeID, float64(x-stream.X), float64(y-stream.Y))
}

func (p *portalInput) Click(x, y int) error {
	if err := p.MoveMouse(x, y); err != nil {
		return err
	}
	const btnLeft = 0x110 // BTN_LEFT (Linux input-event-codes.h)
	if err := p.call("NotifyPointerButton", int32(btnLeft), uint32(1)); err != nil {
		return err
	}
	return p.call("NotifyPointerButton", int32(btnLeft), uint32(0))
}

func (p *portalInput) Scroll(x, y, amount int) error {
	if err := p.MoveMouse(x, y); err != nil {
		return err
	}
	return p.call("NotifyPointerAxis", float64(0), float64(-amount), uint32(0))
}

func (p *portalInput) TypeText(s string) error {
	for _, r := range s {
		units := utf16.Encode([]rune{r})
		for _, u := range units {
			// NotifyKeyboardKeysym takes an evdev-independent X11-style
			// keysym, which matches this package's runeToKeysym helper.
			if err := p.call("NotifyKeyboardKeysym", int32(runeToKeysym(rune(u))), uint32(1)); err != nil {
				return err
			}
			if err := p.call("NotifyKeyboardKeysym", int32(runeToKeysym(rune(u))), uint32(0)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *portalInput) PressKey(key string) error {
	chord, err := ParseChord(key)
	if err != nil {
		return err
	}
	var keysym uint32
	if named, ok := namedKeysyms[chord.Key]; ok {
		keysym = named
	} else {
		runes := []rune(chord.Key)
		if len(runes) != 1 {
			return core.NewInvalidArgsError("unrecognized key name " + chord.Key)
		}
		keysym = runeToKeysym(runes[0])
	}
	if err := p.call("NotifyKeyboardKeysym", int32(keysym), uint32(1)); err != nil {
		return err
	}
	return p.call("NotifyKeyboardKeysym", int32(keysym), uint32(0))
}
