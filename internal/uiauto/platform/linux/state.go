package linux

// AT-SPI2 state bit indices (AtspiStateType from atspi-constants.h). The
// wire value is a 64-bit bitmask split into two little-endian uint32 words
// (state[0] = bits 0-31, state[1] = bits 32-63): bit b lives in
// state[b/32], mask 1<<(b%32).
const (
	stateActive     = 1
	stateArmed      = 2
	stateBusy       = 3
	stateChecked    = 4
	stateCollapsed  = 5
	stateDefunct    = 6
	stateEditable   = 7
	stateEnabled    = 8
	stateExpandable = 9
	stateExpanded   = 10
	stateFocusable  = 11
	stateFocused    = 12
	statePressed    = 20
	stateSelectable = 22
	stateSelected   = 23
	stateSensitive  = 24
	stateShowing    = 25
	stateVisible    = 30
	stateCheckable  = 41
	stateHasPopup   = 42
	stateReadOnly   = 43
)

// hasState reports whether bit b is set in the two-word AT-SPI state
// bitmask. A short or empty state slice (a backend that returned less than
// expected) is treated as "bit not set" rather than panicking.
func hasState(state []uint32, b int) bool {
	word := b / 32
	if word < 0 || word >= len(state) {
		return false
	}
	return state[word]&(1<<uint(b%32)) != 0
}

// decodedState is the subset of AT-SPI state flags this backend interprets,
// both for the normalized Element fields and for Native.Data.
type decodedState struct {
	Enabled    bool
	Sensitive  bool
	Visible    bool
	Showing    bool
	Focused    bool
	Focusable  bool
	Checked    bool
	Selected   bool
	Expanded   bool
	Expandable bool
	Checkable  bool
	Pressed    bool
	Selectable bool
	Editable   bool
	Busy       bool
}

// decodeState decodes the raw AT-SPI []uint32 state bitmask returned by
// Accessible.GetState.
func decodeState(raw []uint32) decodedState {
	return decodedState{
		Enabled:    hasState(raw, stateEnabled),
		Sensitive:  hasState(raw, stateSensitive),
		Visible:    hasState(raw, stateVisible),
		Showing:    hasState(raw, stateShowing),
		Focused:    hasState(raw, stateFocused),
		Focusable:  hasState(raw, stateFocusable),
		Checked:    hasState(raw, stateChecked),
		Selected:   hasState(raw, stateSelected),
		Expanded:   hasState(raw, stateExpanded),
		Expandable: hasState(raw, stateExpandable),
		Checkable:  hasState(raw, stateCheckable),
		Pressed:    hasState(raw, statePressed),
		Selectable: hasState(raw, stateSelectable),
		Editable:   hasState(raw, stateEditable),
		Busy:       hasState(raw, stateBusy),
	}
}

// elementEnabled combines ENABLED and SENSITIVE into the normalized
// Element.Enabled flag: either bit signals the widget accepts interaction.
func (d decodedState) elementEnabled() bool { return d.Enabled || d.Sensitive }

// elementVisible combines VISIBLE and SHOWING into the normalized
// Element.Visible flag: the object must both be visible in principle and
// actually rendered (not scrolled off / obscured / iconified).
func (d decodedState) elementVisible() bool { return d.Visible && d.Showing }

// nativeExtras renders the state flags that do not have a dedicated
// Element field, for storage in NativeData.Data.
func (d decodedState) nativeExtras() map[string]any {
	return map[string]any{
		"focusable":  d.Focusable,
		"checked":    d.Checked,
		"selected":   d.Selected,
		"expanded":   d.Expanded,
		"expandable": d.Expandable,
		"checkable":  d.Checkable,
		"pressed":    d.Pressed,
		"selectable": d.Selectable,
		"editable":   d.Editable,
		"busy":       d.Busy,
	}
}
