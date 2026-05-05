// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockInfo is stored inside the lock file so other instances can find this primary's ports.
type LockInfo struct {
	InstanceID string    `json:"instance_id"`
	PID        int       `json:"pid"`
	PubPort    int       `json:"pub_port"`
	RPCPort    int       `json:"rpc_port"`
	StartedAt  time.Time `json:"started_at"`
}

// lockFilePath returns the path to the IPC lock file for the given workdir.
func lockFilePath(workdir string) string {
	return filepath.Join(workdir, ".pando", "ipc.lock")
}

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

	// Attempt a non-blocking exclusive lock.
	flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr != nil {
		// Another instance holds the lock — read its info.
		_ = f.Close()
		existing, readErr := readLockInfo(path)
		if readErr != nil {
			return false, nil, nil, fmt.Errorf("ipc: read existing lock info: %w", readErr)
		}
		return false, existing, nil, nil
	}

	// We hold the lock — write our info.
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

// writeLockInfo serialises info into f, truncating it first.
func writeLockInfo(f *os.File, info *LockInfo) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

// readLockInfo reads the LockInfo from path without holding the lock.
func readLockInfo(path string) (*LockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse lock file: %w", err)
	}
	return &info, nil
}
