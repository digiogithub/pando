// Package natives ships the native libraries of the desktop UI inside the
// pando binary, so every distribution (CLI zip, Pando.app, .pkg) is one
// self-contained executable.
//
// Today that is libghostty-vt, which MyGo's terminal plugin (plugins/terminal)
// loads with purego. `make natives-<goos>-<goarch>` (scripts/fetch-ghostty-vt)
// writes the build pinned by MyGo's mygo-plugin.json into <goos>-<goarch>/
// before `go build`; on darwin it is signed there with the same Developer ID
// as the pando binary (hardened runtime, timestamp), so library validation
// accepts it without the disable-library-validation entitlement. The
// committed .keep placeholder keeps the embed pattern valid when the library
// was not fetched; such a binary falls back to MyGo's own lookup (Resources,
// executable dir, download into the user cache).
//
// At start-up Install writes the embedded library, byte for byte, to
// <user cache>/pando/natives/<sha256 prefix>/<name> and points
// $MYGO_GHOSTTY_VT at it. MyGo reads that variable lazily, the first time the
// terminal loads, so Install must run before the terminal area is built.
package natives

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
)

// EnvGhosttyVT is the variable MyGo's terminal plugin reads for the path of
// libghostty-vt.
const EnvGhosttyVT = "MYGO_GHOSTTY_VT"

// envOwned records the value Install gave EnvGhosttyVT. Child processes (the
// terminal's shell, tools) inherit both; a pando started from them sees that
// the path came from a pando, possibly another version, and installs its own
// library instead of honouring it.
const envOwned = "PANDO_NATIVES_GHOSTTY_VT"

// ghosttyVTName is the file name MyGo's manifest gives libghostty-vt on goos.
func ghosttyVTName(goos string) string {
	switch goos {
	case "darwin":
		return "libghostty-vt.dylib"
	case "windows":
		return "ghostty-vt.dll"
	default:
		return "libghostty-vt.so"
	}
}

// Embedded reports whether this binary carries libghostty-vt.
func Embedded() bool {
	_, err := fs.Stat(files, path.Join(embedDir, ghosttyVTName(runtime.GOOS)))
	return err == nil
}

// Install extracts the embedded libghostty-vt into the user cache and sets
// $MYGO_GHOSTTY_VT to it, returning the path. It does nothing and returns ""
// when the user set $MYGO_GHOSTTY_VT or the binary embeds no library (MyGo
// then finds or downloads one itself).
func Install() (string, error) {
	if p := os.Getenv(EnvGhosttyVT); p != "" && p != os.Getenv(envOwned) {
		return "", nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	p, err := install(files, path.Join(embedDir, ghosttyVTName(runtime.GOOS)), filepath.Join(cache, "pando", "natives"))
	if err != nil || p == "" {
		return "", err
	}
	if err := os.Setenv(EnvGhosttyVT, p); err != nil {
		return "", err
	}
	_ = os.Setenv(envOwned, p)
	return p, nil
}

// install writes fsys's file name to root/<sha256 prefix>/<base name> unless
// a file with the same content is already there, and returns its path. It
// returns "" when fsys has no such file.
func install(fsys fs.FS, name, root string) (string, error) {
	data, err := fs.ReadFile(fsys, name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	dir := filepath.Join(root, hex.EncodeToString(sum[:])[:16])
	dst := filepath.Join(dir, path.Base(name))

	if fileOK(dst, sum) {
		return dst, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := writeAtomic(dst, data); err != nil {
		return "", err
	}
	// Re-read what landed on disk: a concurrent pando may have won the
	// rename, and a short write must never be loaded.
	if !fileOK(dst, sum) {
		return "", fmt.Errorf("natives: %s does not match the embedded library", dst)
	}
	return dst, nil
}

// fileOK reports whether the file at p exists with SHA-256 sum.
func fileOK(p string, sum [sha256.Size]byte) bool {
	b, err := os.ReadFile(p)
	return err == nil && sha256.Sum256(b) == sum
}

// writeAtomic writes data to a temporary file next to dst and renames it
// over dst, so a reader never sees a partial library.
func writeAtomic(dst string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		// Windows cannot replace a library another process has loaded; one
		// with the right content is as good as ours.
		if sum := sha256.Sum256(data); fileOK(dst, sum) {
			return nil
		}
		return err
	}
	return nil
}
