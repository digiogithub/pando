package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/digiogithub/pando/internal/nativeui/shell"
	"github.com/egoist/mygo/ui"
)

type fakeHost struct {
	mu       sync.Mutex
	focused  bool
	title    string
	trayOn   bool
	projects []shell.TrayProject
	notes    []string
	quit     bool
}

func (h *fakeHost) Focused() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.focused }
func (h *fakeHost) Show()         {}
func (h *fakeHost) Quit()         { h.mu.Lock(); h.quit = true; h.mu.Unlock() }
func (h *fakeHost) SetTitle(t string) {
	h.mu.Lock()
	h.title = t
	h.mu.Unlock()
}
func (h *fakeHost) SetTrayOnClose(on bool) { h.mu.Lock(); h.trayOn = on; h.mu.Unlock() }
func (h *fakeHost) SetTrayProjects(p []shell.TrayProject) {
	h.mu.Lock()
	h.projects = p
	h.mu.Unlock()
}
func (h *fakeHost) Notify(id, title, body string) {
	h.mu.Lock()
	h.notes = append(h.notes, id+"|"+title+"|"+body)
	h.mu.Unlock()
}
func (h *fakeHost) get(f func(*fakeHost)) { h.mu.Lock(); f(h); h.mu.Unlock() }

// fakeExt is a ShellExt with a fixed list of tabs.
type fakeExt struct {
	tabs   []ShellTab
	active string
	closed []string
}

func (e *fakeExt) Tabs() []ShellTab { return e.tabs }
func (e *fakeExt) Active() string   { return e.active }
func (e *fakeExt) Select(id string) { e.active = id }
func (e *fakeExt) Close(id string)  { e.closed = append(e.closed, id) }
func (e *fakeExt) Build(c *ui.Context, id string) {
	ui.Text(c, "workspace "+id)
}
func (e *fakeExt) Dialogs(c *ui.Context) {}

func newRoot(t *testing.T) (*Context, *Root, *ui.Tester, *fakeHost) {
	t.Helper()
	a := NewContext(context.Background(), nil)
	q := &QueueDispatcher{}
	a.SetDispatcher(q)
	h := &fakeHost{}
	a.Shell().Host = h
	root := NewRoot(a)
	return a, root, ui.NewTester(root.View, 1100, 700), h
}

func TestGlobalShortcutsAndWindowTitle(t *testing.T) {
	a, _, tt, h := newRoot(t)
	sh := a.Shell()

	tt.Key(ui.Cmd, ui.KeyK)
	if !sh.QuickMenuOpen {
		t.Fatal("Cmd+K did not open the quick menu")
	}
	sh.QuickMenuOpen = false
	tt.Key(ui.Cmd, ui.KeyP)
	if !sh.QuickMenuOpen {
		t.Fatal("Cmd+P did not open the quick menu")
	}
	sh.QuickMenuOpen = false
	tt.Key(ui.Cmd, ui.KeyO)
	if !sh.ModelSwitcherOpen || sh.QuickMenuOpen {
		t.Fatal("Cmd+O did not open the model switcher alone")
	}
	sh.ModelSwitcherOpen = false

	if sh.SidebarCollapsed {
		t.Fatal("sidebar starts expanded")
	}
	// No server: saving the preference fails quietly in the background.
	tt.Key(ui.Cmd, ui.KeyB)
	if !sh.SidebarCollapsed {
		t.Fatal("Cmd+B did not collapse the sidebar")
	}
	before := a.Theme.Mode
	tt.Key(ui.Cmd|ui.Shift, ui.KeyL)
	if a.Theme.Mode == before {
		t.Fatal("Cmd+Shift+L did not cycle the theme")
	}

	// The window title follows the routed area.
	a.Router.Push("/logs")
	tt.Frame()
	var title string
	h.get(func(h *fakeHost) { title = h.title })
	if title != "Pando — "+a.T("nav.logs") {
		t.Errorf("window title = %q", title)
	}
}

func TestHeaderSlotsAndSlotRegistration(t *testing.T) {
	a, _, tt, _ := newRoot(t)
	RegisterHeaderSlot(HeaderSlot{ID: "test-persona", Order: 10, Build: func(c *ui.Context, a *Context) {
		ui.Text(c, "persona-slot")
	}})
	t.Cleanup(func() {
		slotMu.Lock()
		for i, s := range headerSlots {
			if s.ID == "test-persona" {
				headerSlots = append(headerSlots[:i], headerSlots[i+1:]...)
				break
			}
		}
		slotMu.Unlock()
	})
	tt.Frame()
	if !tt.HasText("persona-slot") {
		t.Fatalf("registered slot not drawn: %q", tt.Texts())
	}
	// Simple chat toggle in the header navigates and the settings icon too.
	if err := tt.Click(a.T("header.simpleChat")); err != nil {
		t.Fatal(err)
	}
	if a.Router.Path() != "/chat/simple" {
		t.Errorf("simple chat toggle: path %q", a.Router.Path())
	}
}

func TestTabShortcutsAndTrayProjects(t *testing.T) {
	a, root, tt, h := newRoot(t)
	ext := &fakeExt{tabs: []ShellTab{{ID: "p1", Title: "One"}, {ID: "p2", Title: "Two"}}}
	root.exts = []ShellExt{ext}
	tt.Frame()
	var got []shell.TrayProject
	h.get(func(h *fakeHost) { got = h.projects })
	if len(got) != 2 || got[0].Name != "One" {
		t.Fatalf("tray projects = %+v", got)
	}

	const mods = ui.Ctrl | ui.Alt
	tt.Key(mods, ui.Key3) // 1 = main, 2 = first project, 3 = second
	if ext.active != "p2" {
		t.Fatalf("Ctrl+Alt+3 focused %q, want p2", ext.active)
	}
	h.get(func(h *fakeHost) {})
	tt.Key(mods, ui.KeyRight) // wraps to main
	if ext.active != "" {
		t.Fatalf("cycle right focused %q, want main", ext.active)
	}
	tt.Key(mods, ui.KeyLeft) // back to the last tab
	if ext.active != "p2" {
		t.Fatalf("cycle left focused %q, want p2", ext.active)
	}
	tt.Key(mods, ui.KeyW)
	if len(ext.closed) != 1 || ext.closed[0] != "p2" {
		t.Fatalf("Ctrl+Alt+W closed %v", ext.closed)
	}
	// The tray entry for a project focuses its tab.
	a.Shell().FocusProject("p1")
	if ext.active != "p1" {
		t.Fatalf("tray focus selected %q, want p1", ext.active)
	}
	tt.Frame()
	h.get(func(h *fakeHost) { got = h.projects })
	if !got[0].Current {
		t.Errorf("current project not marked: %+v", got)
	}
}

func TestNotificationRelayOnlyWhenUnfocused(t *testing.T) {
	a, _, _, h := newRoot(t)
	h.get(func(h *fakeHost) { h.focused = true })
	a.handleNotification(client.Notification{ID: "1", Source: "agent", Message: "done"})
	h.get(func(h *fakeHost) {
		if len(h.notes) != 0 {
			t.Fatalf("notified while focused: %v", h.notes)
		}
		h.focused = false
	})
	a.handleNotification(client.Notification{ID: "2", Source: "agent", Message: "finished"})
	a.handleNotification(client.Notification{ID: "3", Source: "tool", Message: "mcp down"})
	a.handleNotification(client.Notification{ID: "4", Source: "system", Message: "ignored"})
	h.get(func(h *fakeHost) {
		if len(h.notes) != 2 || h.notes[0] != "pando/2|Pando|finished" || h.notes[1] != "pando/3|Pando MCP|mcp down" {
			t.Fatalf("notes = %v", h.notes)
		}
	})
}

func TestApplyPrefsAndServicesFollowSettings(t *testing.T) {
	var mu sync.Mutex
	tray := false
	var put client.UIPrefs
	events := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ui/preferences", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&put)
			mu.Unlock()
		}
		collapsed := true
		_ = json.NewEncoder(w).Encode(client.UIPrefs{Theme: "slate", ThemeMode: "dark", ThemeAccent: "violet", Language: "es", SidebarCollapsed: &collapsed, ChatMode: "simple"})
	})
	mux.HandleFunc("/api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		on := tray
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"desktop_tray_on_minimize_close": on})
	})
	mux.HandleFunc("/api/v1/config/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: {}\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-events:
			_, _ = w.Write([]byte("data: {\"section\":\"Desktop\"}\n\n"))
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("/api/v1/notifications/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := NewContext(ctx, c)
	q := &QueueDispatcher{}
	a.SetDispatcher(q)
	h := &fakeHost{}
	a.StartServices(h)

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			q.Drain()
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timeout waiting for %s", what)
	}
	waitFor("prefs applied", func() bool { return a.Theme.Family == kit.FamilySlate })
	if a.Theme.Mode != kit.ModeDark || a.Theme.Accent != "violet" || !a.Shell().SidebarCollapsed {
		t.Errorf("prefs not applied: %+v collapsed=%v", a.Theme, a.Shell().SidebarCollapsed)
	}
	if a.Router.Path() != "/chat/simple" {
		t.Errorf("persisted simple chat mode not restored: %q", a.Router.Path())
	}
	// Turning the setting on in another surface reaches the shell live.
	var on bool
	waitFor("initial settings", func() bool { h.get(func(h *fakeHost) { on = h.trayOn }); return true })
	if on {
		t.Fatal("tray setting should start off")
	}
	mu.Lock()
	tray = true
	mu.Unlock()
	events <- struct{}{}
	waitFor("tray setting followed", func() bool { h.get(func(h *fakeHost) { on = h.trayOn }); return on })

	// Theme changes persist through the preferences endpoint.
	a.SetTheme(kit.Settings{Family: kit.FamilyForest, Mode: kit.ModeLight})
	waitFor("prefs saved", func() bool { mu.Lock(); defer mu.Unlock(); return put.Theme == "forest" })
	mu.Lock()
	defer mu.Unlock()
	if put.ThemeMode != "light" {
		t.Errorf("saved prefs = %+v", put)
	}
}

func TestReportErrorNamesUnauthorized(t *testing.T) {
	a := NewContext(context.Background(), nil)
	a.ReportError("Could not load", client.ErrUnauthorized)
	a.ReportError("Could not load", client.ErrUnauthorized) // deduplicated
	if got := describeErr(client.ErrUnauthorized); got == "" || got == client.ErrUnauthorized.Error() {
		t.Errorf("401 not described clearly: %q", got)
	}
}

// TestStartThemeFallsBackToBackendTheme: without desktop/web preferences the
// window starts with the backend theme (Settings > Appearance, shared with
// the TUI), not only once that page is opened.
func TestStartThemeFallsBackToBackendTheme(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ui/preferences", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	mux.HandleFunc("/api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"theme":"claude-dark"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := client.New(client.Options{BaseURL: srv.URL, Token: "t"})
	a := NewContext(context.Background(), c)
	q := &QueueDispatcher{}
	a.SetDispatcher(q)
	a.StartServices(nil)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && a.Theme.Family != kit.FamilyPaper {
		q.Drain()
		time.Sleep(10 * time.Millisecond)
	}
	if a.Theme.Family != kit.FamilyPaper || a.Theme.Mode != kit.ModeDark {
		t.Errorf("theme %+v, want paper/dark (legacy claude-dark)", a.Theme)
	}
	if _, _, ok := ParseThemeID("pando-nobg"); ok {
		t.Error("TUI-only theme accepted")
	}
}
