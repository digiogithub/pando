package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxConfigParentDepth bounds the upward search for a project config file so a
// pathological path (or a symlink cycle that survives filepath.Dir) can never
// spin forever.
const maxConfigParentDepth = 128

// localConfigExtensions lists the accepted local config extensions in priority
// order: TOML is the format written by "pando init", JSON is the legacy one.
var localConfigExtensions = []string{"toml", "json"}

// parentConfigSearchEnabled reports whether the upward (parent directory)
// search for a project config file is enabled. It is on by default and can be
// disabled with PANDO_CONFIG_PARENT_SEARCH=false, which restores the previous
// behaviour of only looking at the working directory itself.
//
// The switch is an environment variable on purpose: it decides *which* config
// file is loaded, so it cannot live inside the config file being searched for.
func parentConfigSearchEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("PANDO_CONFIG_PARENT_SEARCH"))
	if raw == "" {
		return true
	}
	return parseEnvBool(raw)
}

// FindLocalConfigFile returns the path of the project-level config file that
// applies to startDir.
//
// It looks for .pando.toml (then .pando.json) in startDir and, when not found
// there, keeps walking up through the parent directories until the filesystem
// root. This lets a single config file at the top of a workspace serve every
// project nested under it.
//
// A candidate is only accepted when the current user can both read and write
// it: an unreadable or read-only file is skipped and the search continues
// upwards, so a config that Pando could not persist changes to is never chosen
// as the active one. Directories that cannot be traversed are skipped for the
// same reason.
//
// The walk stops right after the user's home directory: $HOME/.pando.toml is
// the global (profile) config and is already loaded as such, and anything above
// home is outside the user's own scope. Returns "" when nothing is found.
func FindLocalConfigFile(startDir string) string {
	dir := strings.TrimSpace(startDir)
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	dir = filepath.Clean(abs)

	homeDir := ""
	if h, err := os.UserHomeDir(); err == nil {
		homeDir = filepath.Clean(h)
	}

	for depth := 0; depth < maxConfigParentDepth; depth++ {
		if path := readWritableConfigIn(dir); path != "" {
			return path
		}
		if depth == 0 && !parentConfigSearchEnabled() {
			return ""
		}
		// Above home the only config that applies is the global profile one,
		// which is loaded separately.
		if homeDir != "" && sameDirectory(dir, homeDir) {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}

	return ""
}

// FindLocalConfigDir returns the directory holding the config file that
// FindLocalConfigFile resolves for startDir, or "" when there is none.
func FindLocalConfigDir(startDir string) string {
	if path := FindLocalConfigFile(startDir); path != "" {
		return filepath.Dir(path)
	}
	return ""
}

// readWritableConfigIn returns the path of the first config file in dir that
// the current user can read and write, or "" when dir holds none.
func readWritableConfigIn(dir string) string {
	for _, extension := range localConfigExtensions {
		candidate := filepath.Join(dir, fmt.Sprintf(".%s.%s", appName, extension))
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if !isReadWritable(candidate) {
			continue
		}
		return candidate
	}
	return ""
}

// isReadWritable reports whether the current process can open path for reading
// and for writing. Opening with O_WRONLY (no O_TRUNC, no O_APPEND write) does
// not modify the file, so this probe is side-effect free.
func isReadWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()

	w, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = w.Close()

	return true
}

// sameDirectory compares two directory paths, resolving symlinks when possible
// so that /home/user and a symlinked path to it are treated as the same place.
func sameDirectory(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}
