//go:build !windows

package tlsutil

import (
	"errors"
	"os"
	"syscall"
)

// errLockBusy reports that another process holds the TLS directory lock.
var errLockBusy = errors.New("tls directory lock is busy")

// tryLockFile takes an exclusive flock on f without blocking. flock locks
// belong to the open file description and are released by the kernel when
// the holder exits.
func tryLockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
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
