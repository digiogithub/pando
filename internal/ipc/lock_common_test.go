// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A failover winner rewrites the previous leader's lock file in place; the
// result must be exactly the new document even when the old one was longer.
func TestWriteLockInfoReplacesLongerContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ipc.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	old := &LockInfo{InstanceID: strings.Repeat("x", 200), PID: 1, PubPort: 1, RPCPort: 2, StartedAt: time.Now()}
	if err := writeLockInfo(f, old); err != nil {
		t.Fatal(err)
	}
	want := &LockInfo{InstanceID: "new", PID: 2, PubPort: 3, RPCPort: 4, StartedAt: time.Now().UTC()}
	if err := writeLockInfo(f, want); err != nil {
		t.Fatal(err)
	}

	got, err := readLockInfo(path)
	if err != nil {
		t.Fatalf("readLockInfo after rewrite: %v", err)
	}
	if got.InstanceID != want.InstanceID || got.PID != want.PID || got.PubPort != want.PubPort || got.RPCPort != want.RPCPort {
		t.Fatalf("lock info after rewrite = %+v, want %+v", got, want)
	}
}

// ReadLockForPath must ride out the window in which a starting leader has
// created the lock file but not written it yet, instead of failing to parse it.
func TestReadLockForPathRetriesEmptyFile(t *testing.T) {
	workdir := t.TempDir()
	path := lockFilePath(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	done := make(chan error, 1)
	go func() {
		time.Sleep(3 * lockInfoReadDelay)
		done <- writeLockInfo(f, &LockInfo{InstanceID: "leader", PID: 42, PubPort: 1, RPCPort: 2})
	}()

	info, err := ReadLockForPath(workdir)
	if werr := <-done; werr != nil {
		t.Fatal(werr)
	}
	if err != nil {
		t.Fatalf("ReadLockForPath on a file being written: %v", err)
	}
	if info.PID != 42 {
		t.Fatalf("PID = %d, want 42", info.PID)
	}
}

func TestReadLockForPathMissingFile(t *testing.T) {
	if _, err := ReadLockForPath(t.TempDir()); !os.IsNotExist(err) {
		t.Fatalf("ReadLockForPath without a lock file: err = %v, want not-exist", err)
	}
}
