//go:build windows

package db

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes a shared or exclusive lock on the first byte of f. With
// wait=false it returns errLockBusy instead of blocking when another handle
// holds a conflicting lock. The lock is released by the OS when the handle is
// closed or the process exits.
func lockFile(f *os.File, exclusive, wait bool) error {
	var flags uint32
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLockBusy
	}
	return err
}

func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
