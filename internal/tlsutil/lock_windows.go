//go:build windows

package tlsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// errLockBusy reports that another process holds the TLS directory lock.
var errLockBusy = errors.New("tls directory lock is busy")

// tryLockFile takes an exclusive lock on the first byte of f without
// blocking. The OS releases it when the handle is closed or the process exits.
func tryLockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errLockBusy
	}
	return err
}

func unlockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
