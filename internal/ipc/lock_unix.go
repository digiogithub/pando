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

	f, openErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if openErr != nil {
		return false, nil, nil, fmt.Errorf("ipc: open lock file: %w", openErr)
	}

	flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr != nil {
		_ = f.Close()
		existing, readErr := readLockInfo(path)
		if readErr != nil {
			return false, nil, nil, fmt.Errorf("ipc: read existing lock info: %w", readErr)
		}
		return false, existing, nil, nil
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

// ReleaseLock releases the flock and removes the lock file.
func ReleaseLock(lockFile *os.File) {
	if lockFile == nil {
		return
	}
	path := lockFile.Name()
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	_ = lockFile.Close()
	_ = os.Remove(path)
}