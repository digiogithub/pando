package app

import (
	"sort"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// StatusItem is a part of the window's status strip, under the routed area
// of the main tab (the Web UI's StatusBar slot). Build draws nothing when it
// has nothing to show; the strip itself only appears when an item drew.
type StatusItem struct {
	Order int
	// Visible reports whether the item has something to show this frame.
	Visible func(a *Context) bool
	Build   func(c *ui.Context, a *Context)
}

var statusItems []StatusItem

// RegisterStatusItem adds a status strip item (call from init). The
// extensions package registers the "status-bar" slot here.
func RegisterStatusItem(it StatusItem) {
	slotMu.Lock()
	defer slotMu.Unlock()
	statusItems = append(statusItems, it)
	sort.SliceStable(statusItems, func(i, j int) bool { return statusItems[i].Order < statusItems[j].Order })
}

// statusBar draws the strip with the visible items.
func (r *Root) statusBar(c *ui.Context) {
	slotMu.RLock()
	items := append([]StatusItem(nil), statusItems...)
	slotMu.RUnlock()
	var shown []StatusItem
	for _, it := range items {
		if it.Visible == nil || it.Visible(r.App) {
			shown = append(shown, it)
		}
	}
	if len(shown) == 0 {
		return
	}
	t := kit.Current()
	ui.Row(c.Key("statusbar")).Shrink(0).Gap(8).AlignItems(ui.Center).Padding(0, 8).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.BgShell).Children(func() {
		for i, it := range shown {
			ui.Box(c.Key(i)).Grow(1).Children(func() { it.Build(c, r.App) })
		}
	})
}
