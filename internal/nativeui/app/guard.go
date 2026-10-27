package app

import "github.com/egoist/mygo/ui"

// LeaveGuard is implemented by views that hold unsaved edits (settings). The
// layout asks it before navigating to another area: ConfirmLeave returns
// true when leaving is fine right now; otherwise it shows its own prompt and
// calls proceed later if the user decides to leave.
type LeaveGuard interface {
	ConfirmLeave(proceed func()) bool
}

// SidebarPanel is implemented by area views that put content in the
// sidebar while they are active (the chat: its sessions). SidebarPanel runs
// before the view's Build in every frame, in the room below the navigation;
// collapsed reports the icon-rail sidebar.
type SidebarPanel interface {
	SidebarPanel(c *ui.Context, collapsed bool)
}
