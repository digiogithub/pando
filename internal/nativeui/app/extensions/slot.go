package extensions

import (
	"context"
	"net/url"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// pane hosts one panel in an embedded web view.
type pane struct {
	web      *kit.WebPane
	token    string
	fetching bool
	dark     bool
}

// Slot draws the panels registered for a slot ("sidebar", "settings",
// "chat-side", "status-bar") one under another, or only panelID when given.
// height > 0 fixes each panel's height (use it for compact slots such as the
// status bar); 0 lets a single panel fill the available space. Standard builds
// have no panels and Slot draws nothing, so it can be dropped into a layout
// unconditionally.
func Slot(a *app.Context, c *ui.Context, slot, panelID string, height float32) {
	s := stateFor(a)
	panels := s.bySlot(slot)
	for _, p := range panels {
		if panelID != "" && p.ID != panelID {
			continue
		}
		s.panel(c.Key("ext:"+p.ID), p, height)
	}
}

// panel draws one panel (UI thread).
func (s *state) panel(c *ui.Context, p client.ExtensionPanel, height float32) {
	t := kit.Current()
	pn := s.panes[p.ID]
	if pn == nil {
		pn = &pane{web: &kit.WebPane{Host: s.a.WebViewHost, OnChange: s.a.Invalidate}}
		s.panes[p.ID] = pn
	}
	browser := s.a.Client.BaseURL() + "/ext/" + url.PathEscape(p.ID)
	col := ui.Column(c).AlignSelf(ui.Stretch)
	if height > 0 {
		col.Height(height).Shrink(0)
	} else {
		col.Grow(1)
	}
	col.Children(func() {
		if pn.token == "" {
			if !pn.fetching {
				pn.fetching = true
				s.a.Go(func(ctx context.Context) {
					tok, err := s.a.Client.Token(ctx)
					s.a.Update(func() {
						pn.fetching = false
						if err != nil {
							s.a.ReportError("Extension panel", err)
							return
						}
						pn.token = tok
					})
				})
			}
			ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).TextColor(t.FgFaint) })
			return
		}
		theme := "light"
		if t.Dark {
			theme = "dark"
		}
		if pn.dark != t.Dark && pn.web.Ready() {
			// The host page reads the theme once; follow the app.
			pn.web.Load(HostURL(s.a.Client, p.ID, pn.token, theme), true)
		}
		pn.dark = t.Dark
		pn.web.Load(HostURL(s.a.Client, p.ID, pn.token, theme), false)
		pn.web.Build(c, browser)
	})
}

// HostURL is the address the embedded web view loads for a panel: the
// server-served host page with the API token (a navigation cannot carry a
// header) and the theme.
func HostURL(cl *client.Client, panelID, token, theme string) string {
	u := cl.URL(client.ExtensionHostPath(panelID))
	if token != "" {
		u += "&token=" + url.QueryEscape(token)
	}
	if theme != "" {
		u += "&theme=" + theme
	}
	return u
}
