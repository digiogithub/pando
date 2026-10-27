package app

// LeaveGuard is implemented by views that hold unsaved edits (settings). The
// layout asks it before navigating to another area: ConfirmLeave returns
// true when leaving is fine right now; otherwise it shows its own prompt and
// calls proceed later if the user decides to leave.
type LeaveGuard interface {
	ConfirmLeave(proceed func()) bool
}
