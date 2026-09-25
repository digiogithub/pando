package desktop

import (
	"embed"
	"io/fs"
)

// DesktopBinary is the embedded pre-compiled pando-desktop binary for non-macOS
// builds. Populated by running `make desktop-embed`.
//
// A tracked placeholder keeps a fresh checkout buildable; generated desktop
// binaries remain ignored and are embedded when present. The pattern must not
// match bin/Pando.app: the build tooling recreates it holding only a dotfile,
// which go:embed rejects as a directory with no embeddable files.
//
//go:embed bin/*desktop*
var desktopFiles embed.FS

func EmbeddedDesktopBinary() []byte {
	binary, _ := fs.ReadFile(desktopFiles, "bin/pando-desktop")
	return binary
}
