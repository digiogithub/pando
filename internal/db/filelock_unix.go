//go:build !windows

package db

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes a shared or exclusive advisory lock on f. With wait=false it
// returns errLockBusy instead of blocking when another process holds a
// conflicting lock. flock locks belong to the open file description, so two
// descriptors of the same process conflict exactly like two processes do.
func lockFile(f *os.File, exclusive, wait bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if !wait {
		how |= syscall.LOCK_NB
	}
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errLockBusy
		}
		return err
	}
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
