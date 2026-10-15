// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// writeLockInfo serialises info into f, replacing its previous content.
//
// The document is encoded up front and written at offset 0 before the file is
// truncated to its new length, so the file is never left empty while it is
// rewritten (a failover winner rewrites the previous leader's file in place).
// Readers can still observe a mix of the new document and the tail of a longer
// old one, which is why every reader goes through waitForLockInfoRetry.
func writeLockInfo(f *os.File, info *LockInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := f.WriteAt(data, 0); err != nil {
		return err
	}
	return f.Truncate(int64(len(data)))
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
// the LockInfo written by the primary instance. It returns an error satisfying
// os.IsNotExist when no lock file exists. A file caught mid-rewrite (empty or
// half-written) is retried briefly instead of being reported as unparseable.
func ReadLockForPath(workdir string) (*LockInfo, error) {
	return waitForLockInfoRetry(lockFilePath(workdir))
}

// readHeldLockInfo reads the LockInfo through lockFile, the open lock file this
// process holds as leader (see AcquireLock). Reading through the held handle
// works on every platform, including Windows where the file is held open.
func readHeldLockInfo(lockFile *os.File) (*LockInfo, error) {
	st, err := lockFile.Stat()
	if err != nil {
		return nil, err
	}
	data := make([]byte, st.Size())
	if _, err := lockFile.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse lock file: %w", err)
	}
	return &info, nil
}

// updateLockPorts records pubPort and rpcPort in the lock file this process
// holds as leader, keeping every other field.
func updateLockPorts(lockFile *os.File, pubPort, rpcPort int) error {
	info, err := readHeldLockInfo(lockFile)
	if err != nil {
		return err
	}
	info.PubPort = pubPort
	info.RPCPort = rpcPort
	return writeLockInfo(lockFile, info)
}
