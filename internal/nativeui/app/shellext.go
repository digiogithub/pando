package app

import (
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// ShellTab is a tab of the shell tab bar (a project workspace).
type ShellTab struct {
	ID    string
	Title string
	// Detail is the tooltip (path, state).
	Detail string
	// State is starting, running, stopped or error; it picks the tab icon.
	State string
	// Busy marks a tab whose workspace has an agent run in flight.
	Busy bool
}

// ShellExt adds tabs to the shell. The main tab ("") always exists and shows
// the routed areas; the extension's tabs replace the whole main panel,
// sidebar included, with their own content, the way the Web UI shows a
// project workspace iframe. An extension is created once per root window
// (never for an embedded root) by the factory given to RegisterShellExt.
type ShellExt interface {
	// Tabs lists the open tabs, in order.
	Tabs() []ShellTab
	// Active returns the focused tab ID, "" for the main tab.
	Active() string
	// Select focuses a tab ("" is the main tab).
	Select(id string)
	// Close asks to close a tab (the extension may prompt first).
	Close(id string)
	// Build draws the content of the active tab.
	Build(c *ui.Context, id string)
	// Dialogs draws the extension's modal dialogs; it runs every frame, on
	// whichever tab is focused.
	Dialogs(c *ui.Context)
}

var (
	extMu        sync.Mutex
	extFactories []func(a *Context) ShellExt
)

// RegisterShellExt registers a factory called by NewRoot.
func RegisterShellExt(f func(a *Context) ShellExt) {
	extMu.Lock()
	extFactories = append(extFactories, f)
	extMu.Unlock()
}

func newShellExts(a *Context) []ShellExt {
	extMu.Lock()
	fs := append([]func(*Context) ShellExt{}, extFactories...)
	extMu.Unlock()
	var out []ShellExt
	for _, f := range fs {
		if e := f(a); e != nil {
			out = append(out, e)
		}
	}
	return out
}

// NewEmbeddedRoot returns a root that lives inside another root (a project
// workspace tab): it uses the theme of the host frame, shows no toasts, tab
// bar or theme toggle, and titles its sidebar brand. a is usually made with
// Context.ForClient.
func NewEmbeddedRoot(a *Context, brand string) *Root {
	return &Root{App: a, views: map[string]View{}, embedded: true, brand: brand}
}

func (r *Root) brandName() string {
	if r.brand != "" {
		return r.brand
	}
	return "Pando"
}

// activeExt returns the extension whose tab is focused, nil for the main tab.
func (r *Root) activeExt() ShellExt {
	for _, e := range r.exts {
		if e.Active() != "" {
			return e
		}
	}
	return nil
}

// tabBar draws the main tab and the extensions' tabs; nothing when no
// extension has a tab.
func (r *Root) tabBar(c *ui.Context, t *kit.Tokens) {
	var tabs []struct {
		ext ShellExt
		tab ShellTab
	}
	for _, e := range r.exts {
		for _, tb := range e.Tabs() {
			tabs = append(tabs, struct {
				ext ShellExt
				tab ShellTab
			}{e, tb})
		}
	}
	if len(tabs) == 0 {
		return
	}
	active := ""
	if e := r.activeExt(); e != nil {
		active = e.Active()
	}
	ctl := r.windowControlsActive()
	padR := float32(8)
	if ctl {
		padR = 0
	}
	row := ui.Row(c).Height(36).Shrink(0).Padding(0, padR, 0, 8).Gap(2).AlignItems(ui.Center).Background(t.BgShell).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
	if ctl {
		row.DragWindow() // the tab bar is the title bar
	}
	row.Children(func() {
		r.tabButton(c, t, "main", r.App.T("projects.tabs.main"), kit.IconHome, "running", false, active == "", false, func() {
			for _, e := range r.exts {
				e.Select("")
			}
		}, nil)
		for _, it := range tabs {
			it := it
			icon := kit.IconFolder
			switch it.tab.State {
			case "starting":
				icon = kit.IconSpinner
			case "error":
				icon = kit.IconError
			}
			r.tabButton(c, t, it.tab.ID, it.tab.Title, icon, it.tab.State, it.tab.Busy, active == it.tab.ID, true,
				func() { it.ext.Select(it.tab.ID) }, func() { it.ext.Close(it.tab.ID) })
		}
		if ctl {
			ui.Box(c).Grow(1)
			r.windowControls(c)
		}
	})
}

func (r *Root) tabButton(c *ui.Context, t *kit.Tokens, id, title, icon, state string, busy, active, closable bool, sel, closeFn func()) {
	b := ui.ButtonBase(c.Key("tab:"+id)).Label(title).Height(28).Gap(6).Padding(0, 8).Radius(t.RadiusSM).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).MaxWidth(220).FocusRing(true)
	switch {
	case active:
		b.Background(t.Bg).Border(1, t.Border).TextColor(t.Fg)
	case b.Hovered():
		b.Background(t.BgHover).TextColor(t.Fg)
	default:
		b.TextColor(t.FgMuted)
	}
	b.Children(func() {
		ic := kit.Icon(c, icon).FontSize(14)
		switch {
		case state == "error":
			ic.TextColor(t.Danger)
		case busy:
			ic.TextColor(t.Accent)
		}
		txt := ui.Text(c, title).FontSize(t.TextSM).SingleLine().Ellipsis("…")
		if active {
			txt.FontWeight(600)
		}
		if busy {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(t.Accent)
		}
	})
	if b.Clicked() {
		sel()
	}
	if closable && closeFn != nil {
		if kit.IconButton(c.Key("tabx:"+id), kit.IconClose, r.App.T("projects.tabs.close")).Clicked() {
			closeFn()
		}
	}
}
