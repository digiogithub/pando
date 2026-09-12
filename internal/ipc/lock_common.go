// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

const (
	// lockInfoReadAttempts / lockInfoReadDelay bound the retry used when the lock
	// file cannot be parsed, which is normally a race with the primary rewriting it.
	lockInfoReadAttempts = 10
	lockInfoReadDelay    = 20 * time.Millisecond
)

// lockFilePath returns the path to the IPC lock file for the given workdir.
func lockFilePath(workdir string) string {
	return filepath.Join(workdir, ".pando", "ipc.lock")
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

// waitForLockInfoRetry reads the lock file, retrying briefly while it is
// unreadable or unparseable. The primary truncates the file before rewriting it
// (see writeLockInfo), so a reader that lands inside that window sees an empty or
// half-written file. Without the retry the caller would conclude "there is no
// primary" while another process is actually holding the lock, and would start a
// second primary on the same database.
func waitForLockInfoRetry(path string) (*LockInfo, error) {
	var lastErr error
	for i := 0; i < lockInfoReadAttempts; i++ {
		info, err := readLockInfo(path)
		if err == nil {
			return info, nil
		}
		lastErr = err
		if os.IsNotExist(err) {
			return nil, err
		}
		time.Sleep(lockInfoReadDelay)
	}
	return nil, lastErr
}

// ReadLockForPath reads the IPC lock file for the given workdir and returns
// the LockInfo written by the primary instance. Returns nil if no lock file
// exists or the primary is no longer running.
func ReadLockForPath(workdir string) (*LockInfo, error) {
	return readLockInfo(lockFilePath(workdir))
}
