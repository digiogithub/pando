//go:build linux

package screen

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/portal"
	"github.com/godbus/dbus/v5"
)

// sessionWithStreamsForTest builds a real *portal.Session reporting the
// given streams, via portal.Open backed by fakePortalCaller.
func sessionWithStreamsForTest(t *testing.T, streams ...portal.Stream) *portal.Session {
	t.Helper()
	entries := make([]interface{}, 0, len(streams))
	for _, s := range streams {
		entries = append(entries, []interface{}{s.NodeID, map[string]dbus.Variant{
			"position": dbus.MakeVariant([]interface{}{int32(s.X), int32(s.Y)}),
			"size":     dbus.MakeVariant([]interface{}{int32(s.W), int32(s.H)}),
		}})
	}
	caller := scriptedStreamsCaller{entries: entries}
	sess, err := portal.Open(context.Background(), caller, portal.OpenOptions{WantScreenCast: true})
	if err != nil {
		t.Fatalf("portal.Open: %v", err)
	}
	return sess
}

type scriptedStreamsCaller struct {
	entries []interface{}
}

func (c scriptedStreamsCaller) Request(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) (uint32, map[string]dbus.Variant, error) {
	switch iface + "." + method {
	case portal.RemoteDesktopIface + ".CreateSession":
		return 0, map[string]dbus.Variant{"session_handle": dbus.MakeVariant("/s/test")}, nil
	case portal.RemoteDesktopIface + ".Start":
		return 0, map[string]dbus.Variant{"streams": dbus.MakeVariant(c.entries)}, nil
	default:
		return 0, nil, nil
	}
}
func (c scriptedStreamsCaller) Call(ctx context.Context, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error) {
	return nil, nil
}
func (c scriptedStreamsCaller) Close() error { return nil }

func TestDecodeFileURI(t *testing.T) {
	cases := []struct {
		name    string
		uri     string
		want    string
		wantErr bool
	}{
		{name: "plain", uri: "file:///tmp/shot.png", want: "/tmp/shot.png"},
		{name: "percent-encoded space", uri: "file:///tmp/my%20screenshot.png", want: "/tmp/my screenshot.png"},
		{name: "percent-encoded unicode", uri: "file:///tmp/%C3%A9cran.png", want: "/tmp/écran.png"},
		{name: "authority-less file scheme", uri: "file:/tmp/shot.png", want: "/tmp/shot.png"},
		{name: "not a file uri", uri: "http://example.com/shot.png", wantErr: true},
		{name: "malformed", uri: "not a uri at all", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeFileURI(tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.uri)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeFileURI(%q): %v", tc.uri, err)
			}
			if got != tc.want {
				t.Fatalf("decodeFileURI(%q) = %q, want %q", tc.uri, got, tc.want)
			}
		})
	}
}

func TestDecodePortalScreenshot_DeletesTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture png: %v", err)
	}

	decoded, err := decodePortalScreenshot("file://"+path, Target{})
	if err != nil {
		t.Fatalf("decodePortalScreenshot: %v", err)
	}
	if decoded.Bounds().Dx() != 4 || decoded.Bounds().Dy() != 4 {
		t.Fatalf("decoded bounds = %v", decoded.Bounds())
	}

	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected the portal's temp screenshot file to be deleted, stat err = %v", statErr)
	}
}

func TestDecodePortalScreenshot_CleansUpEvenOnDecodeFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-png.png")
	if err := os.WriteFile(path, []byte("not a png"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := decodePortalScreenshot("file://"+path, Target{})
	if err == nil {
		t.Fatalf("expected a decode error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("temp file must be deleted even when PNG decoding fails, stat err = %v", statErr)
	}
}

func TestDisplays_Wayland_UsesSharedSessionStreams(t *testing.T) {
	t.Cleanup(func() { portal.SetCurrent(nil) })

	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")

	portal.SetCurrent(nil)
	displays, err := Displays()
	if err != nil {
		t.Fatalf("Displays(): %v", err)
	}
	if len(displays) != 1 || displays[0].Name != "wayland-portal" {
		t.Fatalf("with no shared session, Displays() = %+v, want the synthetic placeholder", displays)
	}

	sess := sessionWithStreamsForTest(t, portal.Stream{NodeID: 3, X: 0, Y: 0, W: 1280, H: 720})
	portal.SetCurrent(sess)

	displays, err = Displays()
	if err != nil {
		t.Fatalf("Displays(): %v", err)
	}
	if len(displays) != 1 {
		t.Fatalf("Displays() = %+v, want 1 entry from the shared session's stream", displays)
	}
	if displays[0].Bounds != (core.Bounds{X: 0, Y: 0, W: 1280, H: 720}) {
		t.Fatalf("Displays()[0].Bounds = %+v, want the stream's rectangle", displays[0].Bounds)
	}
	if !displays[0].Primary {
		t.Fatalf("first display should be Primary")
	}
}
