package instances

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func savePNG(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	f, err := os.Create(filepath.Join(os.Getenv("PANDO_NATIVE_SHOT_DIR"), name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// TestShotsReal renders the area against a real server
// (PANDO_NATIVE_IT_URL, CA in PANDO_NATIVE_IT_CA) into PANDO_NATIVE_SHOT_DIR.
// PANDO_NATIVE_IT_PICK selects the instance whose path contains it.
func TestShotsReal(t *testing.T) {
	dir, url := os.Getenv("PANDO_NATIVE_SHOT_DIR"), os.Getenv("PANDO_NATIVE_IT_URL")
	if dir == "" || url == "" {
		t.Skip("PANDO_NATIVE_SHOT_DIR / PANDO_NATIVE_IT_URL not set")
	}
	c, err := client.New(client.Options{BaseURL: url, CAFile: os.Getenv("PANDO_NATIVE_IT_CA")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	root := app.NewRoot(a)
	defer root.Close()
	a.Router.Replace("/instances")
	tt := ui.NewTester(root.View, 1280, 800)
	waitFor(t, tt, q, func() bool { return tt.HasText("PRIMARY") })
	savePNG(t, tt, "instances-real-list.png")
	if pick := os.Getenv("PANDO_NATIVE_IT_PICK"); pick != "" {
		for _, i := range root.App.Client.BaseURL() {
			_ = i
		}
		for _, txt := range tt.Texts() {
			if strings.Contains(txt, pick) {
				if err := tt.Click(txt); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		waitFor(t, tt, q, func() bool { return tt.HasText("No sessions found") || tt.HasText("Sessions (0)") })
		savePNG(t, tt, "instances-real-selected.png")
	}
}

// TestShotsSeeded renders a populated remote session with the fake server of
// TestInstancesView.
func TestShotsSeeded(t *testing.T) {
	if os.Getenv("PANDO_NATIVE_SHOT_DIR") == "" {
		t.Skip("PANDO_NATIVE_SHOT_DIR not set")
	}
	in, tt, q, closeFn := newSeeded(t)
	defer closeFn()
	waitFor(t, tt, q, func() bool { return tt.HasText("PRIMARY") })
	_ = tt.Click("PID 42")
	waitFor(t, tt, q, func() bool { return tt.HasText("Refactor parser") })
	_ = tt.Click("Refactor parser")
	waitFor(t, tt, q, func() bool { return tt.HasText("live") && tt.HasText("streamed-token") })
	savePNG(t, tt, "instances-seeded-stream.png")
	_ = in
}
