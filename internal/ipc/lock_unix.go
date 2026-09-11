// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build !windows

package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Retry bounds for AcquireLock's contended path: when the flock is held but the
// lock file cannot be parsed, the holder is either writing it (a new primary
// that just won) or has just truncated it on release; both resolve quickly.
const (
	lockInfoReadAttempts = 20
	lockInfoReadInterval = 25 * time.Millisecond
	// lockInodeAttempts bounds the retries when the lock file is replaced
	// between our open and our flock (see AcquireLock).
	lockInodeAttempts = 5
)

// AcquireLock tries to acquire an exclusive flock on <workdir>/.pando/ipc.lock.
//
// Returns isPrimary=true if the lock was acquired, false if another instance already
// holds it. If not primary, info contains the connection details of the running primary.
// The caller must call ReleaseLock when done if isPrimary is true.
func AcquireLock(workdir, instanceID string, pubPort, rpcPort int) (isPrimary bool, info *LockInfo, lockFile *os.File, err error) {
	pandoDir := filepath.Join(workdir, ".pando")
	if mkErr := os.MkdirAll(pandoDir, 0o700); mkErr != nil {
		return false, nil, nil, fmt.Errorf("ipc: create .pando directory: %w", mkErr)
	}

	path := lockFilePath(workdir)

	for attempt := 0; ; attempt++ {
		f, openErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if openErr != nil {
			return false, nil, nil, fmt.Errorf("ipc: open lock file: %w", openErr)
		}

		if flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
			_ = f.Close()
			existing, readErr := readLockInfo(path)
			if readErr == nil {
				return false, existing, nil, nil
			}
			// The holder may be rewriting the file, or may have just released
			// (truncated) it. Retry the whole acquisition for a short while
			// instead of failing — a failure here makes Bootstrap run as a
			// primary without IPC.
			if attempt < lockInfoReadAttempts {
				time.Sleep(lockInfoReadInterval)
				continue
			}
			return false, nil, nil, fmt.Errorf("ipc: read existing lock info: %w", readErr)
		}

		// Make sure the file we locked is still the one at path. An older Pando
		// binary's ReleaseLock unlinked the lock file after unlocking it; a
		// process that opened the old inode just before the unlink could then
		// lock that orphaned inode while another process creates and locks a
		// fresh file at the same path — two primaries. If the path no longer
		// refers to our inode, drop it and retry on the current file.
		if !sameFileAtPath(f, path) {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
			if attempt < lockInodeAttempts {
				continue
			}
			return false, nil, nil, fmt.Errorf("ipc: lock file %s keeps being replaced", path)
		}

		li := &LockInfo{
			InstanceID: instanceID,
			PID:        os.Getpid(),
			PubPort:    pubPort,
			RPCPort:    rpcPort,
			StartedAt:  time.Now().UTC(),
		}
		if writeErr := writeLockInfo(f, li); writeErr != nil {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
			return false, nil, nil, fmt.Errorf("ipc: write lock info: %w", writeErr)
		}

		return true, li, f, nil
	}
}

// sameFileAtPath reports whether path currently refers to the same inode as f.
func sameFileAtPath(f *os.File, path string) bool {
	fdInfo, err := f.Stat()
	if err != nil {
		return false
	}
	pathInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(fdInfo, pathInfo)
}

// ReleaseLock releases the flock and closes the lock file.
//
// The file is truncated (while the lock is still held) instead of being
// removed. Unlinking a flock-ed path is racy: a process that opened the old
// inode just before the unlink can lock it while another creates and locks a
// new file at the same path, so both believe they are the primary — a window
// that failover's retry-after-shutdown loop would hit often. Truncating keeps a
// single inode for everyone, and readers of the file (ReadLockForPath) see an
// empty, unparsable file, which they already treat as "no active primary".
func ReleaseLock(lockFile *os.File) {
	if lockFile == nil {
		return
	}
	_ = lockFile.Truncate(0)
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	_ = lockFile.Close()
}
