package extensions

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func fakeServer(t *testing.T, panels string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"token":"tok"}`) })
	mux.HandleFunc("/api/v1/extensions/ui", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, panels) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func setup(t *testing.T, panels string) (*app.Context, *app.QueueDispatcher, *view) {
	t.Helper()
	srv := fakeServer(t, panels)
	cl, err := client.New(client.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, cl)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	stateFor(a).load()
	return a, q, newView(a)
}

func settle(q *app.QueueDispatcher, tt *ui.Tester, cond func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		q.Drain()
		tt.Frame()
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

const twoPanels = `{"panels":[
 {"id":"ui.acme.reports","extension":"ui.acme","title":"Reports","slot":"sidebar","entry":"/ext/acme/panel.js","order":2},
 {"id":"ui.acme.keys","extension":"ui.acme","title":"Keys","slot":"settings","entry":"/ext/acme/keys.js","order":1}]}`

func TestStandardBuildHasNoPanels(t *testing.T) {
	a, q, v := setup(t, `{"panels":[]}`)
	tt := ui.NewTester(func(c *ui.Context) {
		kit.Apply(c, a.Theme)
		a.Router.View(c, func(r *ui.Route) { r.Match("/ext"); v.Build(c, r) })
	}, 900, 600)
	a.Router.Replace("/ext")
	if !settle(q, tt, func() bool { return tt.HasText("No extension panels") }) {
		t.Fatalf("empty state missing: %q", tt.Texts())
	}
	if len(Panels(a, client.SlotSidebar)) != 0 {
		t.Error("standard build reported panels")
	}
	// The sidebar entry hides itself.
	area, _ := app.Lookup("extensions")
	if area.Visible == nil || area.Visible(a) {
		t.Error("extensions entry must be hidden without sidebar panels")
	}
}

func TestPanelsListAndSlots(t *testing.T) {
	a, q, v := setup(t, twoPanels)
	tt := ui.NewTester(func(c *ui.Context) {
		kit.Apply(c, a.Theme)
		a.Router.View(c, func(r *ui.Route) {
			if !r.Match("/ext/{panelId}") {
				r.Match("/ext")
			}
			v.Build(c, r)
		})
	}, 1000, 600)
	a.Router.Replace("/ext/ui.acme.reports")
	if !settle(q, tt, func() bool { return tt.HasText("Reports") && tt.HasText("Keys") }) {
		t.Fatalf("panel list missing: %q", tt.Texts())
	}
	area, _ := app.Lookup("extensions")
	if !area.Visible(a) {
		t.Error("extensions entry hidden although a sidebar panel exists")
	}
	if got := Panels(a, client.SlotSettings); len(got) != 1 || got[0].ID != "ui.acme.keys" {
		t.Errorf("settings slot panels = %+v", got)
	}
	// Order: lower first within the whole manifest.
	all, _ := stateFor(a).all()
	if all[0].ID != "ui.acme.keys" {
		t.Errorf("manifest not sorted by order: %+v", all)
	}
	// With no window to host an embedded view (headless), the panel shows
	// the fallback and the browser button instead of failing.
	if !settle(q, tt, func() bool { return tt.HasText("Open in browser") }) {
		t.Fatalf("fallback missing: %q", tt.Texts())
	}
	// Selecting another panel navigates.
	if err := tt.Click("Keys"); err != nil {
		t.Fatal(err)
	}
	if a.Router.Path() != "/ext/ui.acme.keys" {
		t.Errorf("path = %q", a.Router.Path())
	}
}

func TestSlotDrawsNothingWithoutPanels(t *testing.T) {
	a, q, _ := setup(t, `{"panels":[]}`)
	tt := ui.NewTester(func(c *ui.Context) {
		kit.Apply(c, a.Theme)
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, "before")
			Slot(a, c, client.SlotChatSide, "", 100)
			ui.Text(c, "after")
		})
	}, 400, 300)
	settle(q, tt, func() bool { return false })
	if strings.Contains(strings.Join(tt.Texts(), " "), "Open in browser") {
		t.Error("slot drew something on a standard build")
	}
}

func TestHostURL(t *testing.T) {
	cl, _ := client.New(client.Options{BaseURL: "http://127.0.0.1:8765"})
	got := HostURL(cl, "ui.acme.reports", "a b", "dark")
	want := "http://127.0.0.1:8765/api/v1/extensions/host?panel=ui.acme.reports&token=a+b&theme=dark"
	if got != want {
		t.Errorf("HostURL = %q, want %q", got, want)
	}
}
