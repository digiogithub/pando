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
// exists. The "all:" prefix is required: without it go:embed drops every file
// or directory starting with "_" or ".", which removes
// Contents/_CodeSignature. The extracted bundle then fails code signature
// validation ("invalid resource directory"), so macOS never registers it as an
// app: no activation, no menu bar (Cmd+C/V dead), no status item.
//
//go:embed bin/.keep all:bin/Pando.app
var DesktopBundle embed.FS
