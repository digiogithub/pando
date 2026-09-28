//go:build darwin

package main

// tray is a no-op on macOS for now. The systray library replaces the
// NSApplication delegate that Wails owns, which would break Wails' own
// lifecycle handling, so the menu bar icon needs a dedicated NSStatusItem
// implementation. Until then "minimise to tray" minimises to the Dock.
type tray struct{ app trayActions }

func newTray(app trayActions) *tray { return &tray{app: app} }

func (t *tray) Start() { t.app.SetTrayAvailable(false) }

func (t *tray) Stop() {}
