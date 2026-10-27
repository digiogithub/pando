package setup

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

// TestRealServerWalk drives the wizard headlessly against a real server whose
// HOME is fresh (so setup is needed):
//
//	HOME=<fresh> pando serve --port 18791 &
//	HOME=<fresh> PANDO_NATIVE_IT_URL=https://localhost:18791 PANDO_NATIVE_IT_SHOTS=<dir> \
//	  go test -run TestRealServerWalk ./internal/nativeui/app/setup -v
func TestRealServerWalk(t *testing.T) {
	url := os.Getenv("PANDO_NATIVE_IT_URL")
	if url == "" {
		t.Skip("PANDO_NATIVE_IT_URL not set")
	}
	shots := os.Getenv("PANDO_NATIVE_IT_SHOTS")
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
	tt := ui.NewTester(root.View, 1100, 900)
	shot := func(name string) {
		if shots == "" {
			return
		}
		_ = os.MkdirAll(shots, 0o755)
		f, err := os.Create(filepath.Join(shots, "setup-"+name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		tt.Advance(time.Second)
		tt.Frame()
		_ = png.Encode(f, tt.Image())
	}
	waitUntil(t, q, tt, func() bool { return a.Router.Path() == "/setup" && tt.HasText("Continue") })
	shot("1-scope")
	if err := tt.Click("Continue"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Use these accounts") || tt.HasText("Save and continue") })
	shot("2-provider-existing")
	if tt.HasText("Add another provider") {
		// The server lists auto-detected local runtimes as accounts too.
		if err := tt.Click("Add another provider"); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Connect GitHub Copilot") })
	shot("2-provider")
	if err := tt.Click("Ollama"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Save and continue") })
	shot("3-provider-ollama")
	if err := tt.Click("Save and continue"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Save models") })
	for range 30 {
		q.Drain()
		tt.Frame()
		waitTick()
	}
	shot("4-models")
	if err := tt.Click("Skip"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Check again") })
	shot("5-remembrances")
	if err := tt.Click("Skip"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Finish") })
	shot("6-done")
	if err := tt.Click("Finish"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return a.Router.Path() != "/setup" })
	st, err := c.SetupStatus(ctx)
	if err != nil || st.Needed || !st.Completed {
		t.Fatalf("status after finish %+v %v", st, err)
	}
}
