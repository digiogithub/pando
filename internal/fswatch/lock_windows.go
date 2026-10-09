// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build windows

package fswatch

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes a non-blocking exclusive LockFileEx lock on path. ok is false
// when another handle holds it. The lock is released by the OS if the process
// dies. release is idempotent.
func TryLock(path string) (release func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("fswatch: open lock file: %w", err)
	}
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("fswatch: LockFileEx: %w", err)
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
		_ = f.Close()
	}, true, nil
}
