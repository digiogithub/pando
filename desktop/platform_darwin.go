//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

void pandoInstallAppHooks(void);
*/
import "C"

import "github.com/digiogithub/pando/internal/desktop"

// hookedApp receives the Objective-C callbacks of platform_darwin.m.
var hookedApp *desktop.App

// installPlatformHooks extends the NSApplication delegate Wails owns
// (platform_darwin.m): a Dock icon click restores a minimised or hidden
// window, and a system quit (Dock "Quit", Cmd+Q, logout) is flagged as an
// explicit quit so it is never turned into hide-to-tray.
func installPlatformHooks(app *desktop.App) {
	hookedApp = app
	C.pandoInstallAppHooks()
}

//export pandoAppWillTerminate
func pandoAppWillTerminate() {
	if hookedApp != nil {
		hookedApp.MarkQuitting()
	}
}
