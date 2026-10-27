// Package extensions is the extension UI of the native desktop: the sidebar
// page at /ext/{panelId} and the slots (sidebar, settings, chat-side,
// status-bar) other areas embed.
//
// The Web UI mounts runtime ES-module panels through window.__PANDO_UI__ (web-ui
// src/lib/pandoUI.ts). Natively each panel runs in an embedded web view
// (kit.WebPane) that loads a small host page Pando serves
// (GET /api/v1/extensions/host?panel=<id>, internal/api) providing the same
// __PANDO_UI__ v1 contract. The one thing it cannot provide is ctx.react (the
// shell's React lives in the bundled WebUI): panels that need it show a clear
// error in their box, and "Open in browser" loads the WebUI route /ext/<id>.
//
// For the settings agent and the chat/status areas (see slot.go):
//
//	extensions.Panels(a, client.SlotSettings)      // sections to add ("ext:<id>")
//	extensions.Slot(a, c, client.SlotSettings, id, 0) // draw one panel
//	extensions.Slot(a, c, client.SlotChatSide, "", 220) // every chat-side panel
//	extensions.Slot(a, c, client.SlotStatusBar, "", 28)
package extensions

import (
	"context"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.Register(app.Area{
		ID: "extensions", Path: "/ext", Patterns: []string{"/ext", "/extensions", "/ext/{panelId}"},
		TitleKey: "Extensions", Icon: kit.IconExtensions, Section: app.SectionServices, Order: 1,
		// Standard builds contribute no panels: the entry only shows when
		// there is something behind it.
		Visible: func(a *app.Context) bool { return len(stateFor(a).bySlot(client.SlotSidebar)) > 0 },
		New:     func(a *app.Context) app.View { return newView(a) },
	})
	app.OnStart(func(a *app.Context) { stateFor(a).load() })
	app.RegisterStatusItem(app.StatusItem{
		Visible: func(a *app.Context) bool { return len(stateFor(a).bySlot(client.SlotStatusBar)) > 0 },
		Build:   func(c *ui.Context, a *app.Context) { Slot(a, c, client.SlotStatusBar, "", 28) },
	})
}

// state is the panel manifest of one app context (fetched once: which panels
// exist is decided when the binary is built).
type state struct {
	a *app.Context

	mu     sync.RWMutex
	panels []client.ExtensionPanel
	loaded bool
	err    string

	panes map[string]*pane // by panel id; UI thread only
}

var (
	statesMu sync.Mutex
	states   = map[*app.Context]*state{}
)

func stateFor(a *app.Context) *state {
	statesMu.Lock()
	defer statesMu.Unlock()
	s, ok := states[a]
	if !ok {
		s = &state{a: a, panes: map[string]*pane{}}
		states[a] = s
	}
	return s
}

func (s *state) load() {
	s.a.Go(func(ctx context.Context) {
		panels, err := s.a.Client.ExtensionPanels(ctx)
		s.mu.Lock()
		s.panels, s.loaded = panels, true
		if err != nil {
			// Panels are additive: a failure leaves the shell without
			// them, never without itself.
			s.panels, s.err = nil, err.Error()
		}
		s.mu.Unlock()
		s.a.Invalidate()
	})
}

func (s *state) bySlot(slot string) []client.ExtensionPanel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []client.ExtensionPanel
	for _, p := range s.panels {
		if p.Slot == slot {
			out = append(out, p)
		}
	}
	return out
}

func (s *state) find(id string) (client.ExtensionPanel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.panels {
		if p.ID == id {
			return p, true
		}
	}
	return client.ExtensionPanel{}, false
}

func (s *state) all() ([]client.ExtensionPanel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]client.ExtensionPanel(nil), s.panels...), s.loaded
}

// Panels returns the panels the build contributes to a slot, in order. It is
// empty on standard builds and before the manifest loaded (the settings area
// redraws when it arrives).
func Panels(a *app.Context, slot string) []client.ExtensionPanel {
	return stateFor(a).bySlot(slot)
}

// view is the area: a list of panels on the left, the selected panel on the
// right.
type view struct {
	a    *app.Context
	s    *state
	list ui.ListState
	cur  string
}

func newView(a *app.Context) *view { return &view{a: a, s: stateFor(a)} }

func (v *view) Title() string {
	if p, ok := v.s.find(v.cur); ok && p.Title != "" {
		return p.Title
	}
	return "Extensions"
}

func (v *view) Build(c *ui.Context, r *ui.Route) {
	t := kit.Current()
	panels, loaded := v.s.all()
	id := r.Param("panelId")
	if id == "" && len(panels) > 0 {
		// /ext shows the first sidebar panel.
		for _, p := range panels {
			if p.Slot == client.SlotSidebar {
				id = p.ID
				break
			}
		}
	}
	v.cur = id
	if loaded && len(panels) == 0 {
		kit.EmptyState(c, kit.IconExtensions, "No extension panels",
			"This build of Pando contributes no extension UI.")
		return
	}
	if !loaded {
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).TextColor(t.FgFaint) })
		return
	}
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(240).Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Background(t.BgShell).Children(func() {
			ui.List(c, &v.list, len(panels), func(i int) { v.row(c, t, panels[i]) }).Grow(1).Padding(8).Gap(2)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Padding(8).Children(func() {
			if p, ok := v.s.find(id); ok {
				v.s.panel(c, p, 0)
			} else {
				ui.Text(c, "Unknown extension panel: "+id).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(16)
			}
		})
	})
}

func (v *view) row(c *ui.Context, t *kit.Tokens, p client.ExtensionPanel) {
	title := p.Title
	if title == "" {
		title = p.ID
	}
	row := ui.Column(c).Gap(1).Padding(7, 10).Radius(t.RadiusSM).Cursor(ui.CursorPointer).Focusable().Label(title)
	if row.Clicked() && p.ID != v.cur {
		v.a.Navigate("/ext/" + p.ID)
	}
	if p.ID == v.cur {
		row.Background(t.AccentSoft)
	} else if row.Hovered() {
		row.Background(t.BgHover)
	}
	row.Children(func() {
		ui.Text(c, title).FontSize(t.TextSM).FontWeight(500).SingleLine().Ellipsis("…")
		ui.Text(c, p.Extension+" · "+p.Slot).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine().Ellipsis("…")
	})
}
