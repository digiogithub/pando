//go:build !darwin

package main

import (
	goruntime "runtime"
	"sync"

	"github.com/energye/systray"
)

// tray is the system tray icon: left click restores the window, the menu
// offers Show, Settings and Quit.
type tray struct {
	app      trayActions
	stopOnce sync.Once
	started  bool
}

func newTray(app trayActions) *tray {
	return &tray{app: app}
}

// Start puts the icon in the tray. It runs its own event loop on a dedicated,
// locked OS thread: on Windows the notification window and its message loop
// must share a thread, and on Linux the loop is a D-Bus connection that must
// not block Wails' GTK main loop.
func (t *tray) Start() {
	if !trayHostAvailable() {
		// No tray host (e.g. GNOME without an AppIndicator extension): keep the
		// window minimising to the taskbar so it can always be brought back.
		t.app.SetTrayAvailable(false)
		return
	}
	t.started = true
	go func() {
		goruntime.LockOSThread()
		systray.Run(t.onReady, func() { t.app.SetTrayAvailable(false) })
	}()
}

// Stop removes the icon. Safe to call when Start did not run the tray.
func (t *tray) Stop() {
	if !t.started {
		return
	}
	t.stopOnce.Do(systray.Quit)
}

func (t *tray) onReady() {
	systray.SetIcon(trayIcon())
	systray.SetTitle("Pando")
	systray.SetTooltip("Pando")

	// Left click restores the window; right click keeps the default menu.
	// Handlers leave the tray loop at once: Quit in particular ends up in
	// Stop, which must not run on the loop it is stopping.
	systray.SetOnClick(func(systray.IMenu) { go t.app.ShowWindow() })
	systray.SetOnDClick(func(systray.IMenu) { go t.app.ShowWindow() })

	systray.AddMenuItem("Show Pando", "Bring the Pando window back").Click(func() { go t.app.ShowWindow() })
	systray.AddMenuItem("Settings", "Open Pando settings").Click(func() { go t.app.OpenSettings() })
	systray.AddSeparator()
	systray.AddMenuItem("Quit", "Quit Pando").Click(func() { go t.app.QuitApp() })

	t.app.SetTrayAvailable(true)
}

func trayIcon() []byte {
	if goruntime.GOOS == "windows" {
		return trayIconICO
	}
	return trayIconPNG
}
