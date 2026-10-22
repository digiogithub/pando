//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

void pandoTrayStart(const void *icon, int iconLen);
void pandoTraySetMenu(const char *spec);
void pandoTrayStop(void);
*/
import "C"

import (
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/digiogithub/pando/internal/desktop"
)

// tray is the macOS menu bar icon (NSStatusItem). The energye/systray darwin
// backend replaces the NSApplication delegate that Wails owns, so the status
// item is created directly on top of Wails' run loop instead (tray_darwin.m).
// Clicking the icon opens the menu: Show Pando, Settings, open project
// workspaces and Quit.
type tray struct {
	app     trayActions
	mu      sync.Mutex
	ready   bool
	stopped bool
	state   desktop.ShellState
	actions []func()
}

// activeTray receives the Objective-C callbacks; there is a single tray.
var activeTray *tray

func newTray(app trayActions) *tray {
	t := &tray{app: app}
	app.SetShellStateListener(t.UpdateShellState)
	return t
}

// Start queues the status item creation on the main thread. The icon becomes
// available (and minimise switches to hide-to-tray) once pandoTrayReady runs.
func (t *tray) Start() {
	activeTray = t
	icon := C.CBytes(trayIconPNG)
	defer C.free(icon)
	C.pandoTrayStart(icon, C.int(len(trayIconPNG)))
}

// Stop removes the status item.
func (t *tray) Stop() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	t.ready = false
	t.mu.Unlock()
	t.app.SetTrayAvailable(false)
	C.pandoTrayStop()
}

func (t *tray) UpdateShellState(state desktop.ShellState) {
	t.mu.Lock()
	t.state = state
	ready := t.ready
	t.mu.Unlock()

	if ready {
		t.rebuildMenu()
	}
}

// rebuildMenu serialises the menu as one line per entry ("kind\tid\ttitle",
// kind i = item, d = disabled item, s = separator) and hands it to the main
// thread. Item ids index t.actions.
func (t *tray) rebuildMenu() {
	t.mu.Lock()
	state := t.state
	tabs := append([]desktop.ProjectTabState(nil), state.ProjectTabs...)
	t.mu.Unlock()

	var (
		spec    strings.Builder
		actions []func()
	)
	add := func(title string, fn func()) {
		id := len(actions)
		actions = append(actions, fn)
		spec.WriteString("i\t" + strconv.Itoa(id) + "\t" + menuTitle(title) + "\n")
	}
	separator := func() { spec.WriteString("s\t0\t\n") }

	add("Show Pando", t.app.ShowWindow)
	add("Settings", t.app.OpenSettings)

	if len(tabs) > 0 {
		separator()
		spec.WriteString("d\t0\tProject workspaces\n")
		for _, tab := range tabs {
			title := tab.Name
			if tab.ProjectID == state.ActiveTabID {
				title += " (current)"
			}
			projectID := tab.ProjectID
			add(title, func() { t.app.FocusProjectWorkspace(projectID) })
		}
	}

	separator()
	add("Quit", t.app.QuitApp)

	t.mu.Lock()
	t.actions = actions
	t.mu.Unlock()

	cspec := C.CString(spec.String())
	defer C.free(unsafe.Pointer(cspec))
	C.pandoTraySetMenu(cspec)
}

// menuTitle strips the characters used as field and line separators.
func menuTitle(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}

//export pandoTrayReady
func pandoTrayReady() {
	t := activeTray
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.ready = true
	t.mu.Unlock()
	t.app.SetTrayAvailable(true)
	// Runs on the main thread: leave it before calling back into Objective-C.
	go t.rebuildMenu()
}

//export pandoTrayItemClicked
func pandoTrayItemClicked(id C.int) {
	t := activeTray
	if t == nil {
		return
	}
	t.mu.Lock()
	var fn func()
	if i := int(id); i >= 0 && i < len(t.actions) {
		fn = t.actions[i]
	}
	t.mu.Unlock()
	// Wails runtime calls dispatch to the main thread synchronously; running
	// them on the main thread itself would deadlock.
	if fn != nil {
		go fn()
	}
}
