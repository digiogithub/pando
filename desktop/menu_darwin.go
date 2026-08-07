//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
)

// appMenu builds the macOS application menu.
//
// On macOS the clipboard shortcuts (Cmd+C/V/X/A/Z) are delivered by the
// application menu, not by the webview: WKWebView only receives them when an
// Edit menu with the standard roles exists. Without a menu Wails installs none,
// so copy/paste only works through the native context menu — which is exactly
// the reported symptom. GTK (Linux) and WebView2 (Windows) handle these keys
// internally, hence the platform-specific file.
func appMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())    // Pando: about, hide, quit (Cmd+Q, Cmd+H)
	m.Append(menu.EditMenu())   // undo/redo/cut/copy/paste/select-all
	m.Append(menu.WindowMenu()) // minimize, zoom, full screen
	return m
}
