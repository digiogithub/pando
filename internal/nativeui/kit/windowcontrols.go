package kit

import (
	"github.com/egoist/mygo/ui"
)

// WindowControlsOpts configures WindowControls.
type WindowControlsOpts struct {
	// Maximized shows the restore icon instead of the maximise one.
	Maximized bool
	// Labels are the tooltips and accessible names (already translated).
	Minimize, Maximize, Restore, Close string
}

// WindowControl names a button of WindowControls.
type WindowControl int

// Window controls, in display order.
const (
	ControlNone WindowControl = iota
	ControlMinimize
	ControlMaximize // maximise or restore, whichever the icon shows
	ControlClose
)

// windowControlWidth is the width of a button, as the Web UI's
// .shell-window-btn (44px).
const windowControlWidth = 44

// WindowControls draws the minimise, maximise/restore and close buttons of
// a window without a native title bar, flush to the right edge of its
// parent row (the Web UI's DesktopWindowControls look: 44px wide, full
// height, a red hover on close). It returns the button clicked this frame,
// or ControlNone. Keys are "winctl:*".
func WindowControls(c *ui.Context, o WindowControlsOpts) WindowControl {
	t := Current()
	hit := ControlNone
	btn := func(id string, ctl WindowControl, icon, label string, size float32, danger bool) {
		b := ui.ButtonBase(c.Key("winctl:" + id)).Label(label).Tooltip(label).Width(windowControlWidth).
			AlignSelf(ui.Stretch).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorDefault).FocusRing(true)
		fg := t.FgMuted
		if b.Hovered() || b.Pressed() {
			bg, hfg := t.BgHover, t.Fg
			if danger {
				bg, hfg = ui.Hex("#c42b1c"), ui.Hex("#ffffff")
			}
			b.Background(bg)
			fg = hfg
		}
		b.TextColor(fg)
		b.Children(func() { Icon(c, icon).FontSize(size) })
		if b.Clicked() {
			hit = ctl
		}
	}
	ui.Row(c.Key("winctl")).Shrink(0).AlignSelf(ui.Stretch).Children(func() {
		btn("min", ControlMinimize, IconMinus, o.Minimize, 14, false)
		if o.Maximized {
			btn("max", ControlMaximize, IconRestore, o.Restore, 12, false)
		} else {
			btn("max", ControlMaximize, IconMaximize, o.Maximize, 12, false)
		}
		btn("close", ControlClose, IconClose, o.Close, 14, true)
	})
	return hit
}
