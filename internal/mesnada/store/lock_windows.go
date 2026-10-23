//go:build windows

package store

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockStoreFile takes an exclusive lock on the first byte of path, creating it
// if needed, and returns the function that releases it.
func lockStoreFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
		f.Close()
	}, nil
}
