//go:build darwin

package desktop

import "embed"

// DesktopBundlePlaceholder keeps the bin directory embeddable even when the
// macOS app bundle has not been generated yet.
//
//go:embed bin/.keep
var DesktopBundlePlaceholder []byte

// DesktopBundle contains the embedded macOS .app bundle directory tree.
// Populated by running `make desktop-embed` on macOS.
//
//go:embed bin/Pando.app
//go:embed bin/Pando.app/**
var DesktopBundle embed.FS
