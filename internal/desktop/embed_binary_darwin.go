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
// The directory itself is optional during normal compilation, so keep the
// default embedded filesystem limited to the placeholder file and let runtime
// detection decide whether an app bundle is available.
//
//go:embed bin/.keep
var DesktopBundle embed.FS
