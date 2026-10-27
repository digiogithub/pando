package orchestrator

import (
	"context"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// TestRealServerScreens drives the area against a real server and writes
// PNGs of the detail panel, the create dialog and the cron tab into
// PANDO_NATIVE_IT_SHOTS. Set PANDO_NATIVE_IT_URL to enable.
func TestRealServerScreens(t *testing.T) {
	url, dir := os.Getenv("PANDO_NATIVE_IT_URL"), os.Getenv("PANDO_NATIVE_IT_SHOTS")
	if url == "" || dir == "" {
		t.Skip("PANDO_NATIVE_IT_URL / PANDO_NATIVE_IT_SHOTS not set")
	}
	c, err := client.New(client.Options{BaseURL: url})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	v := New(a)
	tt := ui.NewTester(func(c *ui.Context) { kit.Apply(c, a.Theme); v.Build(c, nil); kit.ToastViewport(c) }, 1300, 760)
	save := func(name string) {
		f, err := os.Create(dir + "/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, tt, q, func() bool { return len(v.tasks) > 0 })
	if err := tt.Click("failed"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	save("orchestrator-detail")
	_ = tt.Click("Create Task")
	tt.Frame()
	save("orchestrator-create")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if err := tt.Click("CronJobs"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return len(v.cron.jobs) > 0 })
	time.Sleep(10 * time.Millisecond)
	tt.Frame()
	save("orchestrator-cron")
	_ = tt.Click("New CronJob")
	tt.Frame()
	save("orchestrator-cron-form")
}
