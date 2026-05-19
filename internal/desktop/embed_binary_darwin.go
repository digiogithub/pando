//go:build darwin

package desktop

import "embed"

// DesktopBundlePlaceholder keeps the bin directory embeddable even when the
// macOS app bundle has not been generated yet.
//
//go:embed bin/.keep
var DesktopBundlePlaceholder []byte

// DesktopBundle contains the embedded macOS .app bundle directory tree when it
// has been copied into internal/desktop/bin/Pando.app by `make desktop-embed`.
//
// The optional placeholder keeps the embed pattern valid before the app bundle
// exists, while the wildcard brings in the bundle contents once generated.
//
//go:embed bin/.keep bin/Pando.app/**
var DesktopBundle embed.FS
