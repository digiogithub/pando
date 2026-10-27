package app

import (
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// windowControlsActive reports whether this root draws the window buttons:
// the main window on a platform without a native title bar (Linux,
// Windows) whose host can drive the window. macOS keeps its traffic lights.
func (r *Root) windowControlsActive() bool {
	return !r.embedded && CustomFrame() && r.App.windowHost() != nil
}

// tabBarShown reports whether the project tab bar is on screen (it then
// stands for the title bar, above the header).
func (r *Root) tabBarShown() bool {
	for _, e := range r.exts {
		if len(e.Tabs()) > 0 {
			return true
		}
	}
	return false
}

// windowControlsInHeader reports whether the header is the title bar.
func (r *Root) windowControlsInHeader() bool {
	return r.windowControlsActive() && !r.tabBarShown()
}

// windowControls draws minimise, maximise/restore and close and forwards
// the click to the host, which applies the tray setting.
func (r *Root) windowControls(c *ui.Context) {
	a := r.App
	h := a.windowHost()
	if h == nil {
		return
	}
	tray := false
	if u, ok := a.Shell().Host.(interface{ UseTray() bool }); ok {
		tray = u.UseTray()
	}
	o := kit.WindowControlsOpts{
		Maximized: h.IsMaximized(),
		Minimize:  a.T("shell.minimise"),
		Maximize:  a.T("shell.maximiseWindow"),
		Restore:   a.T("shell.restoreWindow"),
		Close:     a.T("shell.closeWindow"),
	}
	if tray {
		o.Minimize = a.T("shell.minimiseToTray")
		o.Close = a.T("shell.closeToTray")
	}
	switch kit.WindowControls(c, o) {
	case kit.ControlMinimize:
		h.Minimize()
	case kit.ControlMaximize:
		h.ToggleMaximize()
	case kit.ControlClose:
		h.CloseWindow()
	}
}
