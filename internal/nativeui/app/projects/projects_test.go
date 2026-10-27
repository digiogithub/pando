package projects

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

// probeView stands in for any area (chat, files, ...): it reads the sessions
// through the context's client, which in a workspace is the derived one.
type probeView struct {
	a      *app.Context
	titles []string
	asked  bool
}

func (p *probeView) Build(c *ui.Context, r *ui.Route) {
	if !p.asked {
		p.asked = true
		p.a.Go(func(ctx context.Context) {
			l, err := p.a.Client.ListSessions(ctx, 10, 0)
			if err != nil {
				return
			}
			p.a.Update(func() {
				for _, s := range l.Sessions {
					p.titles = append(p.titles, s.Title)
				}
			})
		})
	}
	for _, s := range p.titles {
		ui.Text(c, s)
	}
}

func init() {
	app.Register(app.Area{ID: "chat", Path: "/chat", TitleKey: "nav.chat",
		New: func(a *app.Context) app.View { return &probeView{a: a} }})
}

type fakeServer struct {
	*httptest.Server
	mu          sync.Mutex
	needsInit   bool
	inited      bool
	opened      int
	childTokens []string
	childHits   int
	stopped     int
}

func newFake(t *testing.T) *fakeServer {
	f := &fakeServer{needsInit: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"token":"parent-tok"}`)
	})
	mux.HandleFunc("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"projects":[{"id":"p1","name":"Alpha","path":"/home/u/alpha","status":"stopped","web_state":"stopped"},`+
			`{"id":"p2","name":"Beta","path":"/home/u/beta","status":"running","web_state":"running","web_port":4321,"delegations":2}]}`)
	})
	mux.HandleFunc("/api/v1/projects/active", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"project":{"id":"p2","name":"Beta","path":"/home/u/beta"}}`)
	})
	mux.HandleFunc("/api/v1/projects/web", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"instances":[]}`) })
	mux.HandleFunc("/api/v1/projects/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: connected\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/api/v1/projects/p1/init", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.inited = true
		f.mu.Unlock()
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/api/v1/projects/p1/web/open", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.inited {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"project_needs_init","path":"/home/u/alpha"}`)
			return
		}
		f.opened++
		fmt.Fprint(w, `{"status":"opened","project_id":"p1","web_port":4000}`)
	})
	mux.HandleFunc("/api/v1/projects/p1/web/close", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.stopped++
		f.mu.Unlock()
		fmt.Fprint(w, `{"status":"closed"}`)
	})
	mux.HandleFunc("/api/v1/projects/p1/web/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.childHits++
		f.childTokens = append(f.childTokens, r.Header.Get(client.HeaderToken))
		f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/projects/p1/web/api/v1/sessions":
			fmt.Fprint(w, `{"sessions":[{"id":"cs1","title":"child-only session title","message_count":1}],"total":1}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func wait(t *testing.T, tt *ui.Tester, q *app.QueueDispatcher, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if q.Drain() > 0 {
			tt.Frame()
		}
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
}

func TestProjectsListAndWorkspaceTab(t *testing.T) {
	f := newFake(t)
	c, err := client.New(client.Options{BaseURL: f.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	a.Router.Replace("/projects")
	root := app.NewRoot(a)
	defer root.Close()
	tt := ui.NewTester(root.View, 1300, 800)

	wait(t, tt, q, "project rows", func() bool { return tt.HasText("Alpha") && tt.HasText("Beta") })
	if !tt.HasText("~/alpha") {
		t.Errorf("path not shortened: %q", tt.Texts())
	}
	if !tt.HasText("Running · :4321") {
		t.Errorf("running badge missing: %q", tt.Texts())
	}

	// Opening an uninitialized project asks to initialize it first.
	if err := tt.Click("Open tab"); err != nil {
		t.Fatal(err)
	}
	wait(t, tt, q, "init dialog", func() bool { return tt.HasText("Initialize project") })
	if err := tt.Click("Initialize & open"); err != nil {
		t.Fatal(err)
	}
	// After init the tab opens and the child's sessions list is shown through
	// the derived client.
	wait(t, tt, q, "child chat", func() bool { return tt.HasText("child-only session title") })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opened != 1 || !f.inited {
		t.Errorf("opened %d inited %v", f.opened, f.inited)
	}
	for _, tok := range f.childTokens {
		if tok != "parent-tok" {
			t.Errorf("child request carried token %q, want the parent's", tok)
		}
	}
}
