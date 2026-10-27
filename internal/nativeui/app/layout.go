package app

import (
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Root is the window's view: sidebar, header and the routed area.
type Root struct {
	App       *Context
	views     map[string]View
	collapsed bool
}

// NewRoot returns the root view of a window.
func NewRoot(a *Context) *Root {
	return &Root{App: a, views: map[string]View{}}
}

// View builds the window. Pass it to ui.View.
func (r *Root) View(c *ui.Context) {
	a := r.App
	t := kit.Apply(c, a.Theme)
	a.Toasts.Flush(c)
	all := Areas()

	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Bg).Children(func() {
		r.sidebar(c, t, all)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			a.Router.View(c, func(route *ui.Route) {
				ar, ok := r.match(route, all)
				if !ok {
					r.header(c, t, "Not found", nil)
					kit.EmptyState(c, kit.IconError, "Not found", route.Path())
					return
				}
				v := r.view(ar)
				title := a.T(ar.TitleKey)
				if tv, ok := v.(Titled); ok {
					if s := tv.Title(); s != "" {
						title = s
					}
				}
				route.Title(title)
				r.header(c, t, title, v)
				ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
					v.Build(c, route)
				})
			})
		})
	})
	kit.ToastViewport(c)
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
	if r.collapsed {
		width = 56
	}
	active := r.active(all)
	ui.Column(c).Width(width).Shrink(0).Background(t.BgShell).BorderWidth(0, 1, 0, 0).
		BorderColor(t.Border).Padding(10, 8).Gap(2).Children(func() {
		// Brand row.
		ui.Row(c).Height(36).Gap(8).Padding(0, 6).AlignItems(ui.Center).Margin(0, 0, 8, 0).Children(func() {
			if !r.collapsed {
				ui.Text(c, "Pando").FontSize(t.TextLG).FontWeight(700).TextColor(t.Fg).Grow(1)
			}
			icon := kit.IconPanelLeft
			if kit.IconButton(c, icon, "Toggle sidebar").Clicked() {
				r.collapsed = !r.collapsed
			}
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			prev := Section("")
			for _, ar := range all {
				if ar.Section == SectionHidden || ar.Section == SectionBottom {
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
		r.themeToggle(c)
	})
}

func (r *Root) navItem(c *ui.Context, ar Area, active string) {
	label := r.App.T(ar.TitleKey)
	if kit.NavItem(c.Key("nav:"+ar.ID), ar.Icon, label, ar.ID == active, r.collapsed).Clicked() {
		if r.App.Router.Path() != ar.Path {
			r.App.Router.Push(ar.Path)
		}
	}
}

// themeToggle cycles system -> light -> dark.
func (r *Root) themeToggle(c *ui.Context) {
	a := r.App
	icon, label := kit.IconMonitor, "Theme: system"
	switch a.Theme.Mode {
	case kit.ModeLight:
		icon, label = kit.IconSun, "Theme: light"
	case kit.ModeDark:
		icon, label = kit.IconMoon, "Theme: dark"
	}
	if kit.NavItem(c.Key("nav:theme"), icon, label, false, r.collapsed).Clicked() {
		switch a.Theme.Mode {
		case kit.ModeLight:
			a.Theme.Mode = kit.ModeDark
		case kit.ModeDark:
			a.Theme.Mode = kit.ModeSystem
		default:
			a.Theme.Mode = kit.ModeLight
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
	})
}
