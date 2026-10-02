//go:build !darwin

package main

import (
	goruntime "runtime"
	"sync"

	"github.com/digiogithub/pando/internal/desktop"
	"github.com/energye/systray"
)

// tray is the system tray icon: left click restores the window, the menu
// offers Show, Settings and Quit.
type tray struct {
	app      trayActions
	stopOnce sync.Once
	started  bool
	mu       sync.Mutex
	ready    bool
	state    desktop.ShellState
}

func newTray(app trayActions) *tray {
	t := &tray{app: app}
	app.SetShellStateListener(t.UpdateShellState)
	return t
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

	t.app.SetTrayAvailable(true)
	t.mu.Lock()
	t.ready = true
	t.mu.Unlock()
	t.rebuildMenu()
}

func trayIcon() []byte {
	if goruntime.GOOS == "windows" {
		return trayIconICO
	}
	return trayIconPNG
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

func (t *tray) snapshotState() desktop.ShellState {
	t.mu.Lock()
	defer t.mu.Unlock()

	snapshot := desktop.ShellState{
		Title:       t.state.Title,
		ActiveTabID: t.state.ActiveTabID,
		ProjectTabs: make([]desktop.ProjectTabState, len(t.state.ProjectTabs)),
	}
	copy(snapshot.ProjectTabs, t.state.ProjectTabs)
	return snapshot
}

func (t *tray) rebuildMenu() {
	state := t.snapshotState()
	systray.ResetMenu()

	systray.AddMenuItem("Show Pando", "Bring the Pando window back").Click(func() { go t.app.ShowWindow() })
	systray.AddMenuItem("Settings", "Open Pando settings").Click(func() { go t.app.OpenSettings() })

	if len(state.ProjectTabs) > 0 {
		systray.AddSeparator()
		section := systray.AddMenuItem("Project workspaces", "Focus an open project workspace tab")
		section.Disable()

		for _, tab := range state.ProjectTabs {
			title := tab.Name
			if tab.ProjectID == state.ActiveTabID {
				title += " (current)"
			}
			projectID := tab.ProjectID
			systray.AddMenuItem(title, "Focus this project workspace").Click(func() {
				go t.app.FocusProjectWorkspace(projectID)
			})
		}
	}

	systray.AddSeparator()
	systray.AddMenuItem("Quit", "Quit Pando").Click(func() { go t.app.QuitApp() })
}
