package redact

import (
	"os"
	"strings"
	"sync"
)

var (
	homeDirOnce sync.Once
	homeDir     string
)

func resolveHomeDir() string {
	homeDirOnce.Do(func() {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
	})
	return homeDir
}

// Path replaces every occurrence of the current user's home directory
// anywhere in s with "~", so file paths embedded in log messages, stack
// traces or attributes never leak the local username. The home directory is
// resolved once (via os.UserHomeDir) and cached for the process lifetime.
func Path(s string) string {
	home := resolveHomeDir()
	// Guard degenerate home values ("" when UserHomeDir failed, or "/" on a
	// minimal container) so we never rewrite every path separator.
	if home == "" || home == "/" || !strings.Contains(s, home) {
		return s
	}
	return strings.ReplaceAll(s, home, "~")
}
