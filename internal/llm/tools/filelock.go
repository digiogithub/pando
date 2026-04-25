package tools

import "sync"

// fileLocks provides per-file mutex locking to prevent concurrent write races.
// A separate mutex is maintained for each absolute file path.
var fileLocks sync.Map // map[string]*sync.Mutex

// withFileLock acquires the mutex for the given file path, executes fn,
// then releases the mutex. This ensures that concurrent edits to the same
// file are serialized. Container bind-mount mode keeps host and container
// paths identical, so the host path remains a stable lock key.
func withFileLock(path string, fn func() error) error {
	mu, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	return fn()
}

// resetFileLocks clears all per-file mutexes. Call at session end to avoid
// memory leaks when file paths are no longer needed.
func resetFileLocks() {
	fileLocks.Range(func(key, _ any) bool {
		fileLocks.Delete(key)
		return true
	})
}
