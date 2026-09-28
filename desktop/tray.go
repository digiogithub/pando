package main

import (
	_ "embed"

	"github.com/digiogithub/pando/internal/desktop"
)

// PNG for the Linux StatusNotifierItem (sent as a pixmap over D-Bus, so it is
// kept small) and ICO for the Windows notification area.
var (
	//go:embed tray/icon.png
	trayIconPNG []byte
	//go:embed tray/icon.ico
	trayIconICO []byte
)

// trayActions are the window operations the tray menu drives.
type trayActions interface {
	ShowWindow()
	OpenSettings()
	QuitApp()
	SetTrayAvailable(bool)
}

var _ trayActions = (*desktop.App)(nil)
