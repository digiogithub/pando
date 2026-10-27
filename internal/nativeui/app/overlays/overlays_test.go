package overlays

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

type env struct {
	a    *app.Context
	q    *app.QueueDispatcher
	tt   *ui.Tester
	mu   sync.Mutex
	puts []map[string]string
	gen  int
}

func newEnv(t *testing.T, initStatus string, version string) *env {
	t.Helper()
	e := &env{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[
			{"id":"auto","name":"Auto","provider":"auto","description":"routes","badges":[],"routerHealthy":true},
			{"id":"m-fast","name":"Fast One","provider":"acme","description":"quick","badges":["fast"],"canReason":true,"contextWindow":200000,"costPer1MIn":1,"costPer1MOut":2},
			{"id":"m-big","name":"Big One","provider":"acme","description":"large","badges":["capable"]}],"autoSelected":false}`))
	})
	mux.HandleFunc("/api/v1/sessions/s1/model", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"s1","model":"m-big","override":true}`))
	})
	mux.HandleFunc("/api/v1/models/active", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		e.mu.Lock()
		e.puts = append(e.puts, b)
		e.mu.Unlock()
		_, _ = w.Write([]byte(`{"model":"` + b["model"] + `","scope":"session"}`))
	})
	mux.HandleFunc("/api/v1/config/provider-types", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"providerTypes":[{"type":"copilot","displayName":"GitHub Copilot","supportsOAuth":true}]}`))
	})
	mux.HandleFunc("/api/v1/config/init-status", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(initStatus)) })
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(version)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.a = app.NewContext(ctx, c)
	e.q = &app.QueueDispatcher{}
	e.a.SetDispatcher(e.q)
	e.tt = ui.NewTester(app.NewRoot(e.a).View, 1100, 700)
	return e
}

func (e *env) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.q.Drain()
		e.tt.Frame()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s; texts=%q", what, e.tt.Texts())
}

func TestQuickMenuFiltersAndNavigates(t *testing.T) {
	e := newEnv(t, `{"shouldGenerate":false}`, `{}`)
	e.tt.Key(ui.Cmd, ui.KeyK)
	if !e.a.Shell().QuickMenuOpen {
		t.Fatal("Cmd+K did not open the quick menu")
	}
	e.until(t, "account commands", func() bool { return e.tt.HasText("Copilot Login") })
	if !e.tt.HasText("Toggle Dark Mode") || !e.tt.HasText("VIEWS") {
		t.Fatalf("palette incomplete: %q", e.tt.Texts())
	}
	e.tt.Type("logs")
	e.tt.Frame()
	if e.tt.HasText("Toggle Dark Mode") {
		t.Errorf("filter kept unrelated commands: %q", e.tt.Texts())
	}
	e.tt.Key(0, ui.KeyEnter)
	e.q.Drain()
	if e.a.Router.Path() != "/logs" || e.a.Shell().QuickMenuOpen {
		t.Fatalf("Enter did not open Logs: path %q open %v", e.a.Router.Path(), e.a.Shell().QuickMenuOpen)
	}
}

func TestQuickMenuCommandRunsOnClick(t *testing.T) {
	e := newEnv(t, `{"shouldGenerate":false}`, `{}`)
	e.tt.Key(ui.Cmd, ui.KeyK)
	e.tt.Type("dark")
	e.tt.Frame()
	before := e.a.Theme.Mode
	if err := e.tt.Click("Toggle Dark Mode"); err != nil {
		t.Fatal(err)
	}
	if e.a.Theme.Mode == before || e.a.Shell().QuickMenuOpen {
		t.Errorf("command did not run: mode %v open %v", e.a.Theme.Mode, e.a.Shell().QuickMenuOpen)
	}
}

func TestModelSwitcherSetsSessionModel(t *testing.T) {
	e := newEnv(t, `{"shouldGenerate":false}`, `{}`)
	e.a.Router.Replace("/chat/s/s1")
	e.tt.Frame()
	e.tt.Key(ui.Cmd, ui.KeyO)
	e.until(t, "models", func() bool { return e.tt.HasText("Fast One") })
	if !e.tt.HasText("Auto") || !e.tt.HasText("thinking") {
		t.Errorf("models list incomplete: %q", e.tt.Texts())
	}
	if err := e.tt.Click("Fast One"); err != nil {
		t.Fatal(err)
	}
	e.until(t, "PUT models/active", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.puts) == 1 })
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.puts[0]["model"] != "m-fast" || e.puts[0]["sessionId"] != "s1" {
		t.Errorf("put = %v, want model m-fast scoped to session s1", e.puts[0])
	}
	if e.a.Shell().ModelSwitcherOpen {
		t.Error("switcher stayed open")
	}
}

func TestConfigInitBannerAndUpdateNotice(t *testing.T) {
	e := newEnv(t, `{"hasLocalConfig":false,"hasPandoDir":false,"shouldGenerate":true}`,
		`{"version":"v1.0.0","latest":"v2.0.0","update_available":true,"checkable":true,"update_command":"pando update","release_url":"https://example.test/r"}`)
	e.until(t, "banners", func() bool {
		return e.tt.HasText("Generate .pando.toml") && e.tt.HasText("Pando v2.0.0 is available (running v1.0.0). Update with: pando update")
	})
	if err := e.tt.Click("Dismiss"); err != nil { // dismisses the first banner
		t.Fatal(err)
	}
	e.tt.Frame()
	if e.tt.HasText("Generate .pando.toml") {
		t.Errorf("config banner not dismissed: %q", e.tt.Texts())
	}
}
