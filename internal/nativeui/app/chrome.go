package app

import (
	"github.com/egoist/mygo/ui"
)

// digitKeys are Ctrl+Alt+1..9, which focus the main tab and the project
// workspace tabs.
var digitKeys = [...]ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// isCollapsed reports the sidebar state: the window's (persisted) for the
// main root, a local one for roots embedded in a project tab.
func (r *Root) isCollapsed() bool {
	if r.embedded {
		return r.collapsed
	}
	return r.App.Shell().SidebarCollapsed
}

func (r *Root) setCollapsed(v bool) {
	if r.embedded {
		r.collapsed = v
		return
	}
	r.App.SetSidebarCollapsed(v)
}

// shortcuts handles the window's global keys, as the Web UI does (Cmd on
// macOS where the Web UI uses Ctrl for the app-level ones):
//
//	Cmd/Ctrl+K, Cmd/Ctrl+P      quick menu
//	Cmd/Ctrl+O                  model switcher
//	Cmd/Ctrl+B                  toggle the sidebar
//	Cmd/Ctrl+Shift+L            cycle the theme
//	Ctrl+Alt+1..9               focus the main / project tab
//	Ctrl+Alt+Left, Right        cycle tabs
//	Ctrl+Alt+W                  close the project tab
func (r *Root) shortcuts(c *ui.Context) {
	a := r.App
	if c.Shortcut(ui.Cmd, ui.KeyK) || c.Shortcut(ui.Cmd, ui.KeyP) {
		a.OpenQuickMenu()
	}
	if c.Shortcut(ui.Cmd, ui.KeyO) {
		a.OpenModelSwitcher()
	}
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		r.setCollapsed(!r.isCollapsed())
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyL) {
		a.CycleThemeMode()
	}
	if len(r.tabIDs()) <= 1 {
		return
	}
	const tabMods = ui.Ctrl | ui.Alt
	for i, k := range digitKeys {
		if c.Shortcut(tabMods, k) {
			r.focusTabIndex(i)
		}
	}
	if c.Shortcut(tabMods, ui.KeyLeft) {
		r.cycleTabs(-1)
	}
	if c.Shortcut(tabMods, ui.KeyRight) {
		r.cycleTabs(1)
	}
	if c.Shortcut(tabMods, ui.KeyW) {
		r.closeActiveTab()
	}
}

// tabIDs lists the tab IDs in order, "" (main) first.
func (r *Root) tabIDs() []string {
	ids := []string{""}
	for _, e := range r.exts {
		for _, t := range e.Tabs() {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func (r *Root) activeTabID() string {
	if e := r.activeExt(); e != nil {
		return e.Active()
	}
	return ""
}

// selectTab focuses a tab ("" is the main one).
func (r *Root) selectTab(id string) {
	for _, e := range r.exts {
		if id == "" {
			e.Select("")
			continue
		}
		for _, t := range e.Tabs() {
			if t.ID == id {
				e.Select(id)
				r.App.Invalidate()
				return
			}
		}
	}
	if id == "" {
		r.App.Invalidate()
	}
}

func (r *Root) focusTabIndex(i int) {
	ids := r.tabIDs()
	if i >= 0 && i < len(ids) {
		r.selectTab(ids[i])
	}
}

func (r *Root) cycleTabs(dir int) {
	ids := r.tabIDs()
	cur := r.activeTabID()
	idx := 0
	for i, id := range ids {
		if id == cur {
			idx = i
		}
	}
	r.selectTab(ids[(idx+dir+len(ids))%len(ids)])
}

func (r *Root) closeActiveTab() {
	e := r.activeExt()
	if e == nil {
		return
	}
	e.Close(e.Active())
	r.App.Invalidate()
}

// focusProject focuses the tab of a project workspace (tray menu); the main
// tab when it is not open.
func (r *Root) focusProject(id string) {
	r.selectTab(id)
	if r.activeTabID() != id {
		r.App.Navigate("/projects")
	}
}

// extTitle is the title of the focused tab of e.
func extTitle(e ShellExt) string {
	if e == nil {
		return ""
	}
	for _, t := range e.Tabs() {
		if t.ID == e.Active() {
			return t.Title
		}
	}
	return ""
}
