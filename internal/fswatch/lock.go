package fswatch

import (
	"fmt"
	"os"
	"path/filepath"
)

// CodeWatchLockPath returns the per-project lock file that guards the startup
// code index and recursive filesystem watcher: <workdir>/.pando/code-watch.lock.
func CodeWatchLockPath(workdir string) string {
	return filepath.Join(workdir, ".pando", "code-watch.lock")
}

// TryLockDir creates <workdir>/.pando if needed and tries to take the
// non-blocking exclusive code-watch lock for workdir. See TryLock.
func TryLockDir(workdir string) (release func(), ok bool, err error) {
	if err := os.MkdirAll(filepath.Join(workdir, ".pando"), 0o700); err != nil {
		return nil, false, fmt.Errorf("fswatch: create .pando directory: %w", err)
	}
	return TryLock(CodeWatchLockPath(workdir))
}
