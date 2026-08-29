// Package screen implements the cross-platform screen capture layer of the
// Pando Desktop Controller, used by Manager.Screenshot
// (internal/uiauto/manager.go). Each platform-specific file
// (screen_windows.go, screen_linux.go, screen_darwin.go, screen_other.go)
// provides its own Capture/Displays/Capabilities implementation, selected
// at compile time by //go:build tags. No file in this package uses cgo.
package screen

import (
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Target selects what to capture: the whole of Display (0 = primary/first
// display when Region and WindowID are both unset), a specific pixel
// Region (screen coordinates), or a specific WindowID (backend-defined
// identifier, as returned by core.WindowInfo.ID). At most one of Region /
// WindowID should be set; when both are empty, the whole Display is
// captured.
type Target struct {
	Display  int
	Region   *core.Bounds
	WindowID string
}

// DisplayInfo describes one physical/logical display.
type DisplayInfo struct {
	Index   int
	Name    string
	Bounds  core.Bounds
	Primary bool
}
