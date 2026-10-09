// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build !windows

package fswatch

import (
	"fmt"
	"os"
	"syscall"
)

// TryLock takes a non-blocking exclusive flock on path. ok is false when
// another open file description (in this or another process) holds it. The
// lock is released automatically if the process dies. release is idempotent.
func TryLock(path string) (release func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("fswatch: open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("fswatch: flock: %w", err)
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		// The file is intentionally left on disk: removing it would race with
		// a contender that already opened the old inode.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}
