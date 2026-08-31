//go:build linux

package screen

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/portal"
	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xinerama"
	"github.com/jezek/xgb/xproto"
)

// This file implements screen capture on Linux. On X11 (or XWayland), it
// uses the core X protocol's GetImage request (github.com/jezek/xgb, no
// cgo), with xinerama for multi-monitor enumeration when the extension is
// present. On a pure Wayland session, it uses the XDG desktop portal's
// org.freedesktop.portal.Screenshot interface over godbus, which writes a
// temporary PNG file and requires interactive user consent; that
// requirement is reported honestly (PERM_DENIED) rather than faked.

func sessionType() string {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if os.Getenv("DISPLAY") != "" {
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

// Capture captures target according to the current session's protocol.
func Capture(ctx context.Context, target Target) (image.Image, error) {
	switch sessionType() {
	case "x11":
		return captureX11(target)
	case "wayland":
		return capturePortal(ctx, target)
	default:
		return nil, core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
	}
}

// Displays enumerates displays according to the current session's protocol.
func Displays() ([]DisplayInfo, error) {
	switch sessionType() {
	case "x11":
		return displaysX11()
	case "wayland":
		// The Screenshot portal itself has no display-enumeration call.
		// When the input package (internal/uiauto/input) has already
		// negotiated the shared RemoteDesktop+ScreenCast session (W1),
		// reuse its real ScreenCast stream geometry (W4) instead of a
		// single synthetic display; only fall back to the synthetic
		// "wayland-portal" placeholder when no such session exists yet, so
		// this call never itself forces a consent dialog.
		if sess := portal.Current(); sess != nil && len(sess.Streams) > 0 {
			out := make([]DisplayInfo, 0, len(sess.Streams))
			for i, st := range sess.Streams {
				out = append(out, DisplayInfo{
					Index:   i,
					Name:    fmt.Sprintf("screencast-node-%d", st.NodeID),
					Bounds:  core.Bounds{X: st.X, Y: st.Y, W: st.W, H: st.H},
					Primary: i == 0,
				})
			}
			return out, nil
		}
		return []DisplayInfo{{Index: 0, Name: "wayland-portal", Primary: true}}, nil
	default:
		return nil, core.NewPlatformNotSupportedError("no X11 or Wayland display session is available (DISPLAY/WAYLAND_DISPLAY unset)")
	}
}

// Capabilities probes what this session can actually capture.
func Capabilities() core.Capabilities {
	switch sessionType() {
	case "x11":
		conn, err := xgb.NewConn()
		if err != nil {
			return core.Capabilities{}
		}
		defer conn.Close()
		// Connecting is not proof that captures work: an X server reached
		// without authority info accepts the connection and then rejects
		// GetImage (BadMatch), and reporting Screenshot=true there tells
		// the model it can take a screenshot that always fails. Probe the
		// real capture path with a 1x1 GetImage instead.
		return core.Capabilities{Screenshot: canGetImage(conn)}
	case "wayland":
		return core.Capabilities{Screenshot: portalAvailable()}
	default:
		return core.Capabilities{}
	}
}

// canGetImage reports whether this X11 connection can actually serve a
// capture, by requesting the smallest possible image from the root window.
// It is the cheapest honest probe of the GetImage path that Capture uses.
func canGetImage(conn *xgb.Conn) bool {
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	_, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(screen.Root), 0, 0, 1, 1, 0xFFFFFFFF).Reply()
	return err == nil
}

// ---- X11 ----

func captureX11(target Target) (image.Image, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, core.NewPlatformNotSupportedError("could not connect to the X11 display: " + err.Error())
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	drawable := xproto.Drawable(screen.Root)

	x, y, w, h := int16(0), int16(0), int(screen.WidthInPixels), int(screen.HeightInPixels)

	if target.WindowID != "" {
		if wid, perr := strconv.ParseUint(target.WindowID, 10, 32); perr == nil {
			drawable = xproto.Drawable(wid)
			geom, gerr := xproto.GetGeometry(conn, drawable).Reply()
			if gerr != nil {
				return nil, core.NewElementNotFoundError("could not query geometry for window " + target.WindowID + ": " + gerr.Error())
			}
			w, h = int(geom.Width), int(geom.Height)
		}
	} else if target.Region != nil {
		x, y = int16(target.Region.X), int16(target.Region.Y)
		w, h = target.Region.W, target.Region.H
	} else if target.Display > 0 {
		displays, derr := displaysX11ForConn(conn)
		if derr == nil && target.Display < len(displays) {
			b := displays[target.Display].Bounds
			x, y, w, h = int16(b.X), int16(b.Y), b.W, b.H
		}
	}
	if w <= 0 || h <= 0 {
		return nil, core.NewInvalidArgsError("capture region has zero or negative size")
	}

	reply, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, drawable, x, y, uint16(w), uint16(h), 0xFFFFFFFF).Reply()
	if err != nil {
		return nil, core.NewActionFailedError("X11 GetImage failed: " + err.Error())
	}
	return decodeZPixmap(reply.Data, w, h)
}

// decodeZPixmap converts a 24/32-bit-per-pixel ZPixmap reply (the
// overwhelmingly common case on modern X servers: little-endian BGRX/BGRA)
// into a Go *image.RGBA.
func decodeZPixmap(data []byte, w, h int) (image.Image, error) {
	total := w * h
	if total <= 0 {
		return nil, core.NewActionFailedError("captured image has zero size")
	}
	bpp := len(data) / total
	if bpp < 3 {
		return nil, core.NewActionFailedError(fmt.Sprintf("unsupported X11 image depth: %d bytes for %d pixels", len(data), total))
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < total; i++ {
		off := i * bpp
		if off+2 >= len(data) {
			break
		}
		b := data[off+0]
		g := data[off+1]
		r := data[off+2]
		img.Pix[i*4+0] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = 0xFF
	}
	return img, nil
}

func displaysX11() ([]DisplayInfo, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, core.NewPlatformNotSupportedError("could not connect to the X11 display: " + err.Error())
	}
	defer conn.Close()
	return displaysX11ForConn(conn)
}

func displaysX11ForConn(conn *xgb.Conn) ([]DisplayInfo, error) {
	if err := xinerama.Init(conn); err == nil {
		if reply, qerr := xinerama.QueryScreens(conn).Reply(); qerr == nil && len(reply.ScreenInfo) > 0 {
			out := make([]DisplayInfo, 0, len(reply.ScreenInfo))
			for i, s := range reply.ScreenInfo {
				out = append(out, DisplayInfo{
					Index:   i,
					Name:    fmt.Sprintf("xinerama-%d", i),
					Bounds:  core.Bounds{X: int(s.XOrg), Y: int(s.YOrg), W: int(s.Width), H: int(s.Height)},
					Primary: i == 0,
				})
			}
			return out, nil
		}
	}
	// No Xinerama (or a single-screen setup): report the default screen as
	// a single display.
	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	return []DisplayInfo{{
		Index:   0,
		Name:    "default",
		Bounds:  core.Bounds{X: 0, Y: 0, W: int(screen.WidthInPixels), H: int(screen.HeightInPixels)},
		Primary: true,
	}}, nil
}

// ---- Wayland (XDG desktop portal Screenshot, via internal/uiauto/portal) ----

// portalAvailable checks whether the portal bus name has an owner, without
// opening a session or requesting consent, purely to answer Capabilities()
// cheaply and honestly.
func portalAvailable() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var owner string
	call := obj.Call("org.freedesktop.DBus.GetNameOwner", 0, portal.BusName)
	if call.Err != nil {
		return false
	}
	return call.Store(&owner) == nil && owner != ""
}

// capturePortal takes a screenshot via the XDG desktop portal's
// org.freedesktop.portal.Screenshot interface, reusing the shared
// internal/uiauto/portal request/response-code machinery (W4): a
// transport-level failure (no portal running) is reported as
// PLATFORM_NOT_SUPPORTED and a user-cancelled consent dialog as
// PERM_DENIED, both via portal.Request/portal.ResponseCodeError, rather
// than a single generic error as before.
func capturePortal(ctx context.Context, target Target) (image.Image, error) {
	caller, err := portal.Dial()
	if err != nil {
		return nil, core.NewPlatformNotSupportedError("could not connect to the D-Bus session bus for the desktop portal: " + err.Error())
	}
	defer caller.Close()

	cctx, cancel := context.WithTimeout(ctx, portal.ConsentTimeout)
	defer cancel()

	results, err := portal.Request(cctx, caller, portal.ObjectPath, portal.ScreenshotIface, "Screenshot", "", map[string]dbus.Variant{
		"interactive":  dbus.MakeVariant(false),
		"handle_token": dbus.MakeVariant(portal.NewHandleToken("pandoreq")),
	})
	if err != nil {
		return nil, err
	}
	uri, _ := results["uri"].Value().(string)
	if uri == "" {
		return nil, core.NewActionFailedError("desktop portal Screenshot response had no uri")
	}
	return decodePortalScreenshot(uri, target)
}

// decodeFileURI robustly percent-decodes a "file://" (or the less common,
// authority-less "file:/path") URI into a filesystem path. url.Parse
// already percent-decodes the standard "file://" form into u.Path; the
// manual fallback below only kicks in for a malformed/non-standard URI
// that url.Parse rejects outright.
func decodeFileURI(raw string) (string, error) {
	if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Scheme, "file") && u.Path != "" {
		return u.Path, nil
	}
	if rest, ok := strings.CutPrefix(raw, "file://"); ok {
		if decoded, err := url.PathUnescape(rest); err == nil {
			return decoded, nil
		}
	}
	if rest, ok := strings.CutPrefix(raw, "file:"); ok {
		if decoded, err := url.PathUnescape(rest); err == nil {
			return decoded, nil
		}
	}
	return "", fmt.Errorf("unsupported or malformed portal screenshot uri: %s", raw)
}

// decodePortalScreenshot reads and decodes the portal's screenshot PNG
// file, then deletes it: the portal writes a real, otherwise-orphaned file
// into the user's filesystem (commonly under $XDG_RUNTIME_DIR or a
// portal-chosen temp/pictures location) for every call, and Pando must not
// leave those behind.
func decodePortalScreenshot(uri string, target Target) (image.Image, error) {
	path, err := decodeFileURI(uri)
	if err != nil {
		return nil, core.NewActionFailedError(err.Error())
	}
	defer func() { _ = os.Remove(path) }()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, core.NewActionFailedError("could not read portal screenshot file: " + err.Error())
	}
	img, err := png.Decode(strings.NewReader(string(data)))
	if err != nil {
		return nil, core.NewActionFailedError("could not decode portal screenshot PNG: " + err.Error())
	}
	if target.Region != nil {
		if rgba, ok := img.(interface {
			SubImage(r image.Rectangle) image.Image
		}); ok {
			r := target.Region
			bounds := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(img.Bounds())
			return rgba.SubImage(bounds), nil
		}
	}
	return img, nil
}
