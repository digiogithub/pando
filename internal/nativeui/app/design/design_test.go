package design

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const artifactJSON = `{"id":"dsg_1","session_id":"ses_9","title":"Landing page","slug":"landing","dir":"design/landing","kind":"web","current_version":2,"created_at":"2026-10-20T10:00:00Z","url":"http://127.0.0.1:1/preview/tok/index.html","bridge_url":"http://127.0.0.1:1/preview/tok/index.html?bridge=1"}`

type fake struct {
	mu       sync.Mutex
	events   chan string
	checkout []string
	critique int
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{events: make(chan string, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"token":"tok"}`) })
	mux.HandleFunc("GET /api/v1/design/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"enabled":true,"preview":true,"renderer":false,"kinds":["web","deck"]}`)
	})
	mux.HandleFunc("GET /api/v1/design/artifacts", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"artifacts":[%s]}`, artifactJSON)
	})
	mux.HandleFunc("GET /api/v1/design/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, artifactJSON) })
	mux.HandleFunc("GET /api/v1/design/artifacts/{id}/screenshot", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"no renderer"}`, http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /api/v1/design/artifacts/{id}/versions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"versions":[{"artifact_id":"dsg_1","number":1,"summary":"first cut","created_at":"2026-10-20T10:00:00Z"},{"artifact_id":"dsg_1","number":2,"summary":"bigger hero","created_at":"2026-10-21T10:00:00Z","critique":{"score":7.5}}]}`)
	})
	mux.HandleFunc("POST /api/v1/design/artifacts/{id}/checkout", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.checkout = append(f.checkout, r.URL.Path)
		f.mu.Unlock()
		fmt.Fprint(w, artifactJSON)
	})
	mux.HandleFunc("GET /api/v1/design/artifacts/{id}/nodes", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"total":2,"nodes":[{"node_id":"n1","role":"heading","text":"Welcome home","selector":"h1.hero","box":{"x":0,"y":0,"w":10,"h":10}},{"node_id":"n2","role":"button","text":"Sign up","selector":"a.cta","box":{"x":0,"y":0,"w":10,"h":10}}]}`)
	})
	mux.HandleFunc("GET /api/v1/design/artifacts/{id}/critique", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"exists":true,"critique":{"id":"c1","artifact_id":"dsg_1","version":2,"score":6.5,"issues":[{"code":"contrast","severity":"warning","node_id":"n2","message":"Low contrast on the button","fix":"Darken the fill"}]},"settings":{"enabled":true,"max_rounds":3,"threshold":8,"policy":"balanced"},"decision":{"pass":false,"iterate":true,"reason":"below the bar","round":1,"max_rounds":3,"score":6.5,"threshold":8,"policy":"balanced"}}`)
	})
	mux.HandleFunc("POST /api/v1/design/artifacts/{id}/critique", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.critique++
		f.mu.Unlock()
		fmt.Fprint(w, `{"rendered":true,"critique":{"id":"c2","artifact_id":"dsg_1","version":2,"score":9.0,"issues":[]},"decision":{"pass":true,"round":2,"max_rounds":3,"score":9,"threshold":8,"policy":"balanced"},"settings":{"enabled":true,"max_rounds":3,"threshold":8,"policy":"balanced"}}`)
	})
	mux.HandleFunc("GET /api/v1/design/skills", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"skills":[{"name":"saas-landing","description":"A SaaS landing page","kind":"web","startable":true,"source":"bundled","example_prompt":"for a CRM"},{"name":"critique-flow","description":"a workflow","startable":false,"source":"bundled"}],"craft":["typography"]}`)
	})
	mux.HandleFunc("GET /api/v1/design/events", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "event: connected\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-f.events:
				fmt.Fprint(w, ev)
				w.(http.Flusher).Flush()
			}
		}
	})
	srv := httptest.NewServer(mux)
	// Cleanups run last-in first-out: the harness cancels its context (ending
	// the SSE stream) before the server closes.
	t.Cleanup(srv.Close)
	return f, srv
}

type harness struct {
	a  *app.Context
	v  *View
	q  *app.QueueDispatcher
	tt *ui.Tester
}

func newHarness(t *testing.T, srv *httptest.Server) *harness {
	t.Helper()
	cl, err := client.New(client.Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, cl)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	v := newView(a)
	t.Cleanup(v.Close)
	storeFor(a).start()
	view := func(c *ui.Context) {
		kit.Apply(c, a.Theme)
		a.Toasts.Flush(c)
		a.Router.View(c, func(r *ui.Route) {
			// The root matches the area patterns before building the view.
			for _, p := range []string{"/design", "/design/{id...}"} {
				if r.Match(p) {
					break
				}
			}
			v.Build(c, r)
		})
		kit.ToastViewport(c)
	}
	return &harness{a: a, v: v, q: q, tt: ui.NewTester(view, 1280, 800)}
}

func (h *harness) wait(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h.q.Drain() > 0 {
			h.tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out; path %s texts %q", h.a.Router.Path(), h.tt.Texts())
}

func TestGalleryAndStudio(t *testing.T) {
	f, srv := newFake(t)
	h := newHarness(t, srv)
	h.a.Router.Replace("/design")

	// The artifact gallery lists the artifact with its kind and version.
	h.wait(t, func() bool { return h.tt.HasText("Landing page") })
	if !h.tt.HasText("Web prototype · v2") {
		t.Errorf("card meta missing: %q", h.tt.Texts())
	}
	// No headless browser: the toolbar says so.
	if !strings.Contains(strings.Join(h.tt.Texts(), "\n"), "No headless browser") {
		t.Errorf("renderer warning missing: %q", h.tt.Texts())
	}

	// Templates tab.
	if err := h.tt.Click("Templates"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, func() bool { return h.tt.HasText("saas-landing") })
	if !h.tt.HasText("Try it") {
		t.Errorf("template actions missing: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Try it"); err != nil {
		t.Fatal(err)
	}
	if h.a.Router.Path() != "/chat" {
		t.Errorf("Try it went to %q", h.a.Router.Path())
	}
	if d := h.a.TakeChatDraft(); !strings.Contains(d, "Use the saas-landing design template. for a CRM") {
		t.Errorf("draft = %q", d)
	}

	// Open the Studio from the route.
	h.a.Router.Replace("/design/dsg_1")
	h.tt.Frame()
	h.wait(t, func() bool { return h.tt.HasText("Welcome home") && h.tt.HasText("Sign up") })
	// Without a window to host a web view the canvas explains and offers the
	// browser instead of failing.
	h.wait(t, func() bool { return h.tt.HasText("Open in browser") })
	if !strings.Contains(strings.Join(h.tt.Texts(), "\n"), "embedded web view is unavailable") {
		t.Errorf("fallback missing: %q", h.tt.Texts())
	}

	// Selecting a node shows the detail and the ask button.
	if err := h.tt.Click("Welcome home"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, func() bool { return h.tt.HasText("design://n1") })
	if err := h.tt.Click("Ask about this element"); err != nil {
		t.Fatal(err)
	}
	if h.a.Router.Path() != "/chat/s/ses_9" {
		t.Errorf("ask went to %q", h.a.Router.Path())
	}
	if d := h.a.TakeChatDraft(); d != "design://n1 (h1.hero)" {
		t.Errorf("draft = %q", d)
	}
	h.a.Router.Replace("/design/dsg_1")
	h.tt.Frame()
	h.wait(t, func() bool { return h.tt.HasText("Issues (1)") })

	// Issues tab: the findings of the last critic pass; clicking one selects
	// its element.
	if err := h.tt.Click("Issues (1)"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, func() bool { return h.tt.HasText("Low contrast on the button") })
	if !h.tt.HasText("6.5/10") {
		t.Errorf("score missing: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Run critique"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, func() bool { return h.tt.HasText("9.0/10") })
	f.mu.Lock()
	if f.critique != 1 {
		t.Errorf("critique runs = %d", f.critique)
	}
	f.mu.Unlock()

	// Version timeline: restore the first version.
	if err := h.tt.Click("Versions"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, func() bool { return h.tt.HasText("first cut") })
	if err := h.tt.Click("Check out"); err != nil {
		t.Fatal(err)
	}
	h.wait(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.checkout) == 1
	})
}

func TestCreatedEventOpensTheArtifact(t *testing.T) {
	f, srv := newFake(t)
	h := newHarness(t, srv)
	h.a.Router.Replace("/chat")
	h.wait(t, func() bool { return h.v.s.status != nil && h.v.s.enabled() })
	// Give the SSE subscription a moment to connect, then create.
	time.Sleep(100 * time.Millisecond)
	f.events <- "event: design.created\ndata: {\"kind\":\"design.created\",\"artifact_id\":\"dsg_1\",\"title\":\"Landing page\"}\n\n"
	h.wait(t, func() bool { return h.a.Router.Path() == "/design/dsg_1" })

	// A repeated event does not steal the view again.
	h.a.Router.Replace("/chat")
	f.events <- "event: design.created\ndata: {\"kind\":\"design.created\",\"artifact_id\":\"dsg_1\"}\n\n"
	time.Sleep(150 * time.Millisecond)
	h.q.Drain()
	if h.a.Router.Path() != "/chat" {
		t.Errorf("repeated created event navigated to %q", h.a.Router.Path())
	}
}

func TestDisabledNotice(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"token":"tok"}`) })
	mux.HandleFunc("GET /api/v1/design/status", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"enabled":false}`) })
	mux.HandleFunc("GET /api/v1/design/events", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	h := newHarness(t, srv)
	h.a.Router.Replace("/design")
	h.wait(t, func() bool { return h.tt.HasText("The Design Studio is off") })
}

func TestBridgeMessages(t *testing.T) {
	_, srv := newFake(t)
	h := newHarness(t, srv)
	s := h.v.s
	cv := newCanvas(s)
	cv.apply([]bridgeMsg{{Type: "ready"}, {Type: "selected", NodeID: "n7", Tag: "h1", Text: "Hi", Slide: 2}, {Type: "slide", Slide: 3}})
	if !cv.ready || s.sel == nil || s.sel.NodeID != "n7" || s.sel.Selection != "design://n7" || s.sel.Slide != 2 || s.slide != 3 {
		t.Fatalf("state after bridge reports: ready=%v sel=%+v slide=%d", cv.ready, s.sel, s.slide)
	}
	// The page's own report is not echoed back to it.
	if cv.sentSel != "n7" || cv.sentSlide != 3 {
		t.Errorf("echo guards = %q %d", cv.sentSel, cv.sentSlide)
	}
	cv.apply([]bridgeMsg{{Type: "selected"}})
	if s.sel != nil {
		t.Error("empty selection did not clear")
	}
	if !strings.Contains(preloadJS, bridgeSource) || !strings.Contains(drainJS, "splice") {
		t.Error("preload/drain scripts lost the envelope")
	}
}

func TestRebase(t *testing.T) {
	cases := []struct{ raw, base, want string }{
		{"http://127.0.0.1:9/preview/t/index.html?bridge=1", "http://127.0.0.1:8765", "http://127.0.0.1:9/preview/t/index.html?bridge=1"},
		{"http://127.0.0.1:9/preview/t/index.html?bridge=1", "https://pando.example.com:8443", "https://pando.example.com:8443/preview/t/index.html?bridge=1"},
		{"https://cdn.example.com/x", "https://pando.example.com", "https://cdn.example.com/x"},
		{"", "http://x", ""},
	}
	for _, c := range cases {
		if got := rebase(c.raw, c.base); got != c.want {
			t.Errorf("rebase(%q, %q) = %q, want %q", c.raw, c.base, got, c.want)
		}
	}
}

func TestSaveDownloadDoesNotOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p1, err := saveDownload("a.png", []byte("1"))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := saveDownload("a.png", []byte("2"))
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 || !strings.HasSuffix(p2, "a (1).png") {
		t.Errorf("paths %q %q", p1, p2)
	}
}
