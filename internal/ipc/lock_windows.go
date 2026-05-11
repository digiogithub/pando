// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build windows

package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AcquireLock tries to acquire an exclusive lock on <workdir>/.pando/ipc.lock.
//
// On Windows, keeping the file open after opening it read-write provides the
// single-instance guarantee because subsequent opens requesting write access fail.
func AcquireLock(workdir, instanceID string, pubPort, rpcPort int) (isPrimary bool, info *LockInfo, lockFile *os.File, err error) {
	pandoDir := filepath.Join(workdir, ".pando")
	if mkErr := os.MkdirAll(pandoDir, 0o700); mkErr != nil {
		return false, nil, nil, fmt.Errorf("ipc: create .pando directory: %w", mkErr)
	}

	path := lockFilePath(workdir)

	f, openErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if openErr != nil {
		if os.IsExist(openErr) {
			existing, readErr := waitForLockInfo(path)
			if readErr != nil {
				return false, nil, nil, fmt.Errorf("ipc: read existing lock info: %w", readErr)
			}
			return false, existing, nil, nil
		}
		return false, nil, nil, fmt.Errorf("ipc: open lock file: %w", openErr)
	}

	li := &LockInfo{
		InstanceID: instanceID,
		PID:        os.Getpid(),
		PubPort:    pubPort,
		RPCPort:    rpcPort,
		StartedAt:  time.Now().UTC(),
	}
	if writeErr := writeLockInfo(f, li); writeErr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return false, nil, nil, fmt.Errorf("ipc: write lock info: %w", writeErr)
	}

	return true, li, f, nil
}

// ReleaseLock closes and removes the lock file.
func ReleaseLock(lockFile *os.File) {
	if lockFile == nil {
		return
	}
	path := lockFile.Name()
	_ = lockFile.Close()
	_ = os.Remove(path)
}

func waitForLockInfo(path string) (*LockInfo, error) {
	var lastErr error
	for i := 0; i < 20; i++ {
		info, err := readLockInfo(path)
		if err == nil {
			return info, nil
		}
		if os.IsNotExist(err) {
			lastErr = err
			break
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, os.ErrNotExist
}