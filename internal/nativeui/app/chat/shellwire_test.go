package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

const (
	pongID = "2bc571c2-eeab-41de-883d-2deafa7eefdb"
	helpID = "add84465-1793-42c2-8c97-3c403dd9d2d4"
)

// TestChatShellWiring covers the chat's integration with the window: session
// clicks and Back follow the route, Shift+Tab toggles auto-approve, the model
// switcher's NextModel goes with the first prompt, ModelChanged updates the
// session model, the design draft fills the composer and the persona
// selector sits in the header.
func TestChatShellWiring(t *testing.T) {
	base := fakeAPI(t)
	defer base.Close()
	var (
		mu       sync.Mutex
		models   []string
		approves []string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sessions/{id}/auto-approve", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		approves = append(approves, r.PathValue("id")+" "+string(b))
		mu.Unlock()
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/v1/sessions/{id}/auto-approve", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"enabled":false}`))
	})
	mux.HandleFunc("GET /api/v1/personas", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"personas":["architect"]}`)) })
	mux.HandleFunc("GET /api/v1/personas/active", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"active":"architect","auto":false}`))
	})
	mux.HandleFunc("POST /api/v1/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req client.ChatRequest
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		models = append(models, req.Model)
		mu.Unlock()
		r.Body = io.NopCloser(strings.NewReader(string(b)))
		base.Config.Handler.ServeHTTP(w, r)
	})
	mux.Handle("/", base.Config.Handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := client.New(client.Options{BaseURL: srv.URL})
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
	tt := ui.NewTester(root.View, 1400, 900)
	waitFor(t, tt, q, func() bool { return tt.HasText("pong") })
	ch := shownChat(a)
	if ch == nil {
		t.Fatal("chat not registered for the header")
	}

	// Persona selector in the header (not in the composer).
	waitFor(t, tt, q, func() bool { return tt.HasText("Architect") })
	if ch.model.open || ch.persona.open || tt.HasText("Switch Model") {
		t.Fatalf("a picker dialog opened by itself: model %v persona %v", ch.model.open, ch.persona.open)
	}

	// A session click updates the route; Back returns to the previous one.
	if err := tt.Click("pong"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return a.Router.Path() == "/chat/s/"+pongID && ch.current == pongID })
	if err := tt.Click("/help"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return a.Router.Path() == "/chat/s/"+helpID && ch.current == helpID })
	a.Router.Back()
	waitFor(t, tt, q, func() bool { tt.Frame(); return ch.current == pongID })
	if got := a.ActiveSessionID(); got != pongID {
		t.Errorf("ActiveSessionID %q", got)
	}

	// Shift+Tab toggles auto-approve of the open session.
	tt.Key(ui.Shift, ui.KeyTab)
	waitFor(t, tt, q, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(approves) == 1
	})
	if !strings.Contains(approves[0], pongID) || !strings.Contains(approves[0], `"enabled":true`) {
		t.Errorf("auto-approve call %q", approves[0])
	}

	// The model switcher changed the session's model.
	a.Shell().ModelChanged("gpt-5", pongID)
	if ch.model.session[pongID].Model != "gpt-5" {
		t.Errorf("session model %+v", ch.model.session[pongID])
	}

	// The design draft fills the composer when the chat is shown.
	a.InsertIntoChatDraft("Ask about this element")
	tt.Frame()
	if ch.draft != "Ask about this element" || a.PeekChatDraft() != "" {
		t.Errorf("draft %q, pending %q", ch.draft, a.PeekChatDraft())
	}
	ch.draft = ""

	// New chat: route back to /chat; the switcher's NextModel goes with the
	// first prompt and is then cleared.
	if err := tt.Click("New session"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, q, func() bool { return a.Router.Path() == "/chat" && ch.current == "" })
	a.Shell().NextModel = "claude-opus"
	tt.Frame()
	tt.Type("hello")
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, q, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(models) == 1
	})
	if models[0] != "claude-opus" || a.Shell().NextModel != "" {
		t.Errorf("model sent %q, NextModel left %q", models[0], a.Shell().NextModel)
	}
	// The new session lands in the route.
	waitFor(t, tt, q, func() bool { return a.Router.Path() == "/chat/s/new-1" })
}
