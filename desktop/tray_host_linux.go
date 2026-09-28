//go:build linux

package main

import (
	"github.com/godbus/dbus/v5"
)

// trayHostAvailable reports whether the session has a StatusNotifierItem host,
// the D-Bus protocol the tray icon uses. KDE, XFCE, Cinnamon, and GNOME with
// the AppIndicator extension provide one; stock GNOME does not.
func trayHostAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	watcher := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	v, err := watcher.GetProperty("org.kde.StatusNotifierWatcher.IsStatusNotifierHostRegistered")
	if err != nil {
		return false
	}
	registered, ok := v.Value().(bool)
	return ok && registered
}
