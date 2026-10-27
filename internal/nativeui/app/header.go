package app

import (
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// DocsURL is the documentation site the header links to.
const DocsURL = "https://madeindigio.github.io/pando-docs/"

// OpenExternal opens a URL in the default browser (a variable for tests).
var OpenExternal = func(url string) { _ = mygo.Shell.OpenExternal(url) }

// The built-in header controls, in the Web UI's order. Other areas add theirs
// with RegisterHeaderSlot (the persona selector uses ID "persona", order 10).
func init() {
	RegisterHeaderSlot(HeaderSlot{ID: "simple-chat", Order: 100, Build: func(c *ui.Context, a *Context) {
		active := a.Router.Path() == "/chat/simple"
		label := a.T("header.simpleChat")
		if kit.Button(c, kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconSimpleChat, Tooltip: label, Active: active}).Clicked() {
			a.ToggleSimpleChat()
		}
	}})
	RegisterHeaderSlot(HeaderSlot{ID: "docs", Order: 110, Build: func(c *ui.Context, a *Context) {
		if kit.IconButton(c, kit.IconBookOpen, a.T("shell.documentation")).Clicked() {
			OpenExternal(DocsURL)
		}
	}})
	RegisterHeaderSlot(HeaderSlot{ID: "theme", Order: 120, Build: func(c *ui.Context, a *Context) {
		icon, label := kit.IconMonitor, a.T("header.toggleTheme")
		switch a.Theme.Mode {
		case kit.ModeLight:
			icon, label = kit.IconSun, a.T("shell.switchToDark")
		case kit.ModeDark:
			icon, label = kit.IconMoon, a.T("shell.switchToLight")
		}
		if kit.IconButton(c, icon, label).Clicked() {
			a.CycleThemeMode()
		}
	}})
	RegisterHeaderSlot(HeaderSlot{ID: "settings", Order: 130, Build: func(c *ui.Context, a *Context) {
		if kit.IconButton(c, kit.IconSettings, a.T("nav.settings")).Clicked() && a.Router.Path() != "/settings" {
			a.Navigate("/settings")
		}
	}})
}

// headerSlots draws the registered header controls (main window only).
func (r *Root) headerSlots(c *ui.Context) {
	for _, s := range sortedHeaderSlots() {
		s.Build(c.Key("hdr:"+s.ID), r.App)
	}
}
