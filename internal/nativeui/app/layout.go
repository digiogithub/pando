package app

import (
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Root is the window's view: sidebar, header and the routed area.
type Root struct {
	App       *Context
	views     map[string]View
	collapsed bool
	started   bool // start hooks ran (startup.go)

	embedded bool       // hosted in a project tab: no theme, toasts or tab bar (shellext.go)
	brand    string     // sidebar title (default "Pando")
	exts     []ShellExt // shell extensions (project tabs)

	overlays []Overlay // dialogs above the window (quick menu, model switcher)
	banners  []Banner  // strips above the main tab's header
	title    string    // title of the frame's page, for the window title
}

// NewRoot returns the root view of a window.
func NewRoot(a *Context) *Root {
	r := &Root{App: a, views: map[string]View{}}
	r.exts = newShellExts(a)
	r.overlays = newOverlays(a)
	r.banners = newBanners(a)
	if sh := a.Shell(); sh.FocusProject == nil {
		sh.FocusProject = r.focusProject
	}
	return r
}

// View builds the window. Pass it to ui.View.
func (r *Root) View(c *ui.Context) {
	a := r.App
	if !r.embedded {
		r.runStartHooks()
	}
	r.title = ""
	var t *kit.Tokens
	if r.embedded {
		t = kit.Current()
	} else {
		t = kit.Apply(c, a.Theme)
		a.Toasts.Flush(c)
		r.shortcuts(c)
	}
	all := Areas()
	ext := r.activeExt()
	if len(r.exts) > 0 {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Background(t.Bg).Children(func() {
			r.tabBar(c, t)
			ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				if ext != nil {
					ext.Build(c, ext.Active())
				} else {
					r.main(c, t, all)
				}
			})
		})
	} else {
		r.main(c, t, all)
	}
	for _, e := range r.exts {
		e.Dialogs(c)
	}
	if !r.embedded {
		for _, o := range r.overlays {
			o.Build(c)
		}
		kit.ToastViewport(c)
		if ext != nil {
			r.title = extTitle(ext)
		}
		r.syncChrome(r.title)
	}
}

// main builds the sidebar and the routed area.
func (r *Root) main(c *ui.Context, t *kit.Tokens, all []Area) {
	a := r.App
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Bg).Children(func() {
		r.sidebar(c, t, all)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			if !r.embedded {
				for _, b := range r.banners {
					b.Build(c)
				}
			}
			a.Router.View(c, func(route *ui.Route) {
				ar, ok := r.match(route, all)
				if !ok {
					r.header(c, t, "Not found", nil)
					kit.EmptyState(c, kit.IconError, "Not found", route.Path())
					return
				}
				if r.leavingSameArea(route, ar, all) {
					// MyGo builds the page going away while the next one
					// slides in; an area view is one stateful object (its
					// ListStates panic when shown by two Lists in a
					// frame), so only the page shown builds it.
					ui.Column(c).Grow(1).Background(t.Bg)
					return
				}
				v := r.view(ar)
				title := a.T(ar.TitleKey)
				if tv, ok := v.(Titled); ok {
					if s := tv.Title(); s != "" {
						title = s
					}
				}
				if !r.embedded {
					route.Title(title)
					r.title = title
				}
				r.header(c, t, title, v)
				ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
					v.Build(c, route)
				})
			})
			if !r.embedded {
				r.statusBar(c)
			}
		})
	})
}

func (r *Root) match(route *ui.Route, all []Area) (Area, bool) {
	for _, ar := range all {
		for _, p := range ar.Patterns {
			if route.Match(p) {
				return ar, true
			}
		}
		if ar.Path != "" && route.Match(ar.Path) {
			return ar, true
		}
	}
	return Area{}, false
}

// leavingSameArea reports whether route is the page going away during a
// route transition between two routes of the same area (/chat to
// /chat/s/{id}, /snapshots/x to /snapshots/y).
func (r *Root) leavingSameArea(route *ui.Route, ar Area, all []Area) bool {
	if samePath(route.Path(), r.App.Router.Path()) {
		return false
	}
	return ar.ID == r.active(all)
}

// samePath compares two router paths ignoring empty parts (trailing or
// doubled slashes).
func samePath(a, b string) bool {
	clean := func(p string) string {
		parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
		return strings.Join(parts, "/")
	}
	return clean(a) == clean(b)
}

// view returns the area's view, building it on first use.
func (r *Root) view(ar Area) View {
	if v, ok := r.views[ar.ID]; ok {
		return v
	}
	v := ar.New(r.App)
	r.views[ar.ID] = v
	return v
}

// Close stops the views that own background work.
func (r *Root) Close() {
	for _, v := range r.views {
		if cl, ok := v.(Closer); ok {
			cl.Close()
		}
	}
}

// Active returns the area currently routed, by path prefix.
func (r *Root) active(all []Area) string {
	path := r.App.Router.Path()
	best, bestLen := "", -1
	for _, ar := range all {
		if ar.Path == path {
			return ar.ID
		}
		if len(ar.Path) > bestLen && len(path) > len(ar.Path) && path[:len(ar.Path)] == ar.Path && path[len(ar.Path)] == '/' {
			best, bestLen = ar.ID, len(ar.Path)
		}
	}
	return best
}

func (r *Root) sidebar(c *ui.Context, t *kit.Tokens, all []Area) {
	width := float32(216)
	if r.isCollapsed() {
		width = 56
	}
	active := r.active(all)
	ui.Column(c).Width(width).Shrink(0).Background(t.BgShell).BorderWidth(0, 1, 0, 0).
		BorderColor(t.Border).Padding(10, 8).Gap(2).Children(func() {
		// Brand row.
		ui.Row(c).Height(36).Gap(8).Padding(0, 6).AlignItems(ui.Center).Margin(0, 0, 8, 0).Children(func() {
			if !r.isCollapsed() {
				ui.Text(c, r.brandName()).FontSize(t.TextLG).FontWeight(700).TextColor(t.Fg).Grow(1)
			}
			icon := kit.IconPanelLeft
			if kit.IconButton(c, icon, r.App.T("header.toggleSidebar")).Clicked() {
				r.setCollapsed(!r.isCollapsed())
			}
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			prev := Section("")
			for _, ar := range all {
				if ar.Section == SectionHidden || ar.Section == SectionBottom || (r.embedded && ar.ID == "projects") {
					continue
				}
				if ar.Visible != nil && !ar.Visible(r.App) {
					continue
				}
				if prev != "" && ar.Section != prev {
					ui.Box(c).Height(1).Margin(6, 4).Background(t.Border)
				}
				prev = ar.Section
				r.navItem(c, ar, active)
			}
		})
		for _, ar := range all {
			if ar.Section == SectionBottom {
				r.navItem(c, ar, active)
			}
		}
	})
}

func (r *Root) navItem(c *ui.Context, ar Area, active string) {
	label := r.App.T(ar.TitleKey)
	if kit.NavItem(c.Key("nav:"+ar.ID), ar.Icon, label, ar.ID == active, r.isCollapsed()).Clicked() {
		if r.App.Router.Path() != ar.Path {
			go_ := func() { r.App.Router.Push(ar.Path) }
			if g, ok := r.views[active].(LeaveGuard); ok && active != ar.ID && !g.ConfirmLeave(go_) {
				return
			}
			go_()
		}
	}
}

func (r *Root) header(c *ui.Context, t *kit.Tokens, title string, v View) {
	ui.Row(c).Height(48).Shrink(0).Padding(0, 16).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, title).FontSize(t.TextMD).FontWeight(600).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		if ha, ok := v.(HeaderActions); ok {
			ha.HeaderActions(c)
		}
		if !r.embedded {
			r.headerSlots(c)
		}
	})
}
