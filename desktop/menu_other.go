//go:build !darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
)

// appMenu returns no menu outside macOS: GTK and WebView2 already implement the
// clipboard shortcuts, and an extra menu bar would only take vertical space.
func appMenu() *menu.Menu { return nil }
