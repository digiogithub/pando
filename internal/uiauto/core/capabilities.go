package core

import (
	"sort"
	"strings"
)

// Capabilities describes what a Backend can actually do on the current
// platform/session. Callers must check these rather than branching on
// platform name, since e.g. Wayland sessions may lack input/screenshot
// capabilities even though the backend itself is available.
type Capabilities struct {
	Screenshot       bool `json:"screenshot"`
	Accessibility    bool `json:"accessibility"`
	UIInspection     bool `json:"uiInspection"`
	Mouse            bool `json:"mouse"`
	Keyboard         bool `json:"keyboard"`
	WindowManagement bool `json:"windowManagement"`
	UIActions        bool `json:"uiActions"`
	Events           bool `json:"events"`
}

// fieldValue reports whether the named capability field is set. name is
// case-insensitive.
func (c Capabilities) fieldValue(name string) (bool, bool) {
	switch strings.ToLower(name) {
	case "screenshot":
		return c.Screenshot, true
	case "accessibility":
		return c.Accessibility, true
	case "uiinspection":
		return c.UIInspection, true
	case "mouse":
		return c.Mouse, true
	case "keyboard":
		return c.Keyboard, true
	case "windowmanagement":
		return c.WindowManagement, true
	case "uiactions":
		return c.UIActions, true
	case "events":
		return c.Events, true
	default:
		return false, false
	}
}

// Missing returns the subset of required capability names (case-insensitive
// field names such as "screenshot", "uiActions") that are not enabled on c.
// An unrecognized name is reported as missing too, so callers can catch
// typos.
func (c Capabilities) Missing(required ...string) []string {
	var missing []string
	for _, name := range required {
		val, known := c.fieldValue(name)
		if !known || !val {
			missing = append(missing, name)
		}
	}
	return missing
}

// String renders the enabled capabilities as a comma-separated,
// alphabetically sorted list, e.g. "accessibility,keyboard,mouse".
func (c Capabilities) String() string {
	all := map[string]bool{
		"screenshot":       c.Screenshot,
		"accessibility":    c.Accessibility,
		"uiInspection":     c.UIInspection,
		"mouse":            c.Mouse,
		"keyboard":         c.Keyboard,
		"windowManagement": c.WindowManagement,
		"uiActions":        c.UIActions,
		"events":           c.Events,
	}
	var enabled []string
	for k, v := range all {
		if v {
			enabled = append(enabled, k)
		}
	}
	sort.Strings(enabled)
	if len(enabled) == 0 {
		return "(none)"
	}
	return strings.Join(enabled, ",")
}
