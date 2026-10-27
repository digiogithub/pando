package evaluator

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func savePNG(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("PANDO_NATIVE_SHOT_DIR")
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// TestShots renders the area to PNG files (screenshots): with
// PANDO_NATIVE_SHOT_DIR alone against the seeded fake server, plus the real
// server when PANDO_NATIVE_IT_URL is set (PANDO_NATIVE_IT_CA = its CA file).
func TestShots(t *testing.T) {
	dir := os.Getenv("PANDO_NATIVE_SHOT_DIR")
	if dir == "" {
		t.Skip("PANDO_NATIVE_SHOT_DIR not set")
	}
	run := func(c *client.Client, prefix string, deep bool) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := app.NewContext(ctx, c)
		q := &app.QueueDispatcher{}
		a.SetDispatcher(q)
		root := app.NewRoot(a)
		a.Router.Replace("/evaluator")
		tt := ui.NewTester(root.View, 1280, 900)
		waitFor(t, tt, q, func() bool { return tt.HasText("Sessions Evaluated") })
		savePNG(t, tt, prefix+"-variants.png")
		if deep {
			if err := tt.Click("Sessions (1)"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, tt, q, func() bool { return tt.HasText("Fix the bug") })
			_ = tt.Click("Fix the bug")
			tt.Frame()
			savePNG(t, tt, prefix+"-sessions.png")
		}
	}
	var reviewed string
	run(fakeServer(t, &reviewed), "evaluator-seeded", true)
	if url := os.Getenv("PANDO_NATIVE_IT_URL"); url != "" {
		c, err := client.New(client.Options{BaseURL: url, CAFile: os.Getenv("PANDO_NATIVE_IT_CA")})
		if err != nil {
			t.Fatal(err)
		}
		run(c, "evaluator-real", false)
	}
}
