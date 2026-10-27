package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

type fakeAPI struct {
	mu       sync.Mutex
	needed   bool
	accounts []map[string]any
	models   map[string]string
	complete bool
	scope    string
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	js := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		js(w, client.SetupStatus{WorkingDir: "/home/u/proj", Needed: f.needed && !f.complete, Completed: f.complete})
	})
	mux.HandleFunc("/api/v1/setup/scope", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.scope = b["scope"]
		f.mu.Unlock()
		js(w, client.SetupScopeResult{Scope: b["scope"], ConfigPath: "/home/u/.config/pando/.pando.toml", Status: client.SetupStatus{WorkingDir: "/home/u/proj", Needed: true}})
	})
	mux.HandleFunc("/api/v1/config/provider-accounts", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodPost {
			var a map[string]any
			_ = json.NewDecoder(r.Body).Decode(&a)
			f.accounts = append(f.accounts, a)
			js(w, a)
			return
		}
		js(w, map[string]any{"providerAccounts": f.accounts})
	})
	mux.HandleFunc("/api/v1/setup/suggested-models", func(w http.ResponseWriter, r *http.Request) {
		js(w, client.SuggestedModels{MainModel: "openai.gpt-x", FastModel: "openai.gpt-mini"})
	})
	mux.HandleFunc("/api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		js(w, client.ModelList{Models: []client.Model{{ID: "openai.gpt-x"}, {ID: "openai.gpt-mini"}}})
	})
	mux.HandleFunc("/api/v1/setup/models", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.models = b
		f.mu.Unlock()
		js(w, client.SuggestedModels{MainModel: b["mainModel"], FastModel: b["fastModel"]})
	})
	mux.HandleFunc("/api/v1/setup/ollama/status", func(w http.ResponseWriter, r *http.Request) {
		js(w, client.SetupOllamaStatus{BaseURL: "http://localhost:11434", Installed: false, DocumentModel: "d", CodeModel: "c",
			InstallOptions: []client.SetupOllamaOption{{ID: "download", Label: "dl", URL: "https://ollama.com", Recommended: true}}})
	})
	mux.HandleFunc("/api/v1/setup/complete", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.complete = true
		f.mu.Unlock()
		js(w, client.SetupStatus{Completed: true})
	})
	return mux
}

func setup(t *testing.T, f *fakeAPI) (*app.Context, *app.QueueDispatcher, *ui.Tester) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	root := app.NewRoot(a)
	tt := ui.NewTester(root.View, 1100, 800)
	return a, q, tt
}

func waitUntil(t *testing.T, q *app.QueueDispatcher, tt *ui.Tester, cond func() bool) {
	t.Helper()
	for range 400 {
		q.Drain()
		tt.Frame()
		if cond() {
			return
		}
		waitTick()
	}
	t.Fatalf("condition not met; texts: %q", tt.Texts())
}

func TestWizardOpensWhenNeededAndWalksSteps(t *testing.T) {
	f := &fakeAPI{needed: true}
	a, q, tt := setup(t, f)
	waitUntil(t, q, tt, func() bool { return a.Router.Path() == "/setup" && tt.HasText("This directory has no Pando configuration. Where should the settings you choose now be saved?") })

	if err := tt.Click("Continue"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Choose the AI provider Pando will use. You can add more later in Settings > Providers.") })
	if f.scope != "global" {
		t.Errorf("scope %q", f.scope)
	}
	// Pick OpenAI, type a key, save.
	if err := tt.Click("OpenAI"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	r, ok := tt.Find("API key")
	if !ok {
		t.Fatalf("no API key label: %q", tt.Texts())
	}
	tt.ClickAt(r.X+30, r.Y+r.H+24)
	tt.Type("sk-test")
	if err := tt.Click("Save and continue"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Save models") && tt.HasText("openai.gpt-mini") })
	f.mu.Lock()
	n := len(f.accounts)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("accounts posted: %d", n)
	}
	if err := tt.Click("Save models"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Ollama") && tt.HasText("Skip") })
	if f.models["mainModel"] != "openai.gpt-x" {
		t.Errorf("models %v", f.models)
	}
	if err := tt.Click("Skip"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return tt.HasText("Finish") })
	if err := tt.Click("Finish"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, q, tt, func() bool { return a.Router.Path() == "/chat" })
	if !f.complete {
		t.Error("complete not called")
	}
}

func TestWizardStaysClosedWhenNotNeeded(t *testing.T) {
	f := &fakeAPI{needed: false}
	a, q, tt := setup(t, f)
	for range 20 {
		q.Drain()
		tt.Frame()
		waitTick()
	}
	if a.Router.Path() == "/setup" {
		t.Fatal("wizard opened though setup is not needed")
	}
}

func TestCancelReturnsToPreviousPath(t *testing.T) {
	f := &fakeAPI{needed: true}
	a, q, tt := setup(t, f)
	waitUntil(t, q, tt, func() bool { return a.Router.Path() == "/setup" && tt.HasText("Cancel assistant") })
	if err := tt.Click("Cancel assistant"); err != nil {
		t.Fatal(err)
	}
	q.Drain()
	tt.Frame()
	if a.Router.Path() != "/chat" {
		t.Errorf("path %q", a.Router.Path())
	}
}

func waitTick() { time.Sleep(5 * time.Millisecond) }
