// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build !windows

package ipc

import (
	"os"
	"testing"
)

// TestReleaseLockTruncatesInsteadOfUnlinking verifies the lock file survives a
// release (one inode for everyone, no unlink race), reads as "no primary", and
// can be re-acquired by the next instance.
func TestReleaseLockTruncatesInsteadOfUnlinking(t *testing.T) {
	dir := t.TempDir()
	isPrimary, _, f, err := AcquireLock(dir, "first", 43000, 43001)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock: primary=%v err=%v", isPrimary, err)
	}
	ReleaseLock(f)

	st, err := os.Stat(lockFilePath(dir))
	if err != nil {
		t.Fatalf("lock file removed on release: %v", err)
	}
	if st.Size() != 0 {
		t.Fatalf("released lock file has %d bytes, want 0", st.Size())
	}
	if info, err := ReadLockForPath(dir); err == nil && info != nil {
		t.Fatalf("released lock file still reads as held by %+v", info)
	}

	isPrimary, info, f2, err := AcquireLock(dir, "second", 43000, 43001)
	if err != nil || !isPrimary {
		t.Fatalf("re-acquire after release: primary=%v err=%v", isPrimary, err)
	}
	defer ReleaseLock(f2)
	if info.InstanceID != "second" {
		t.Fatalf("lock info instance = %q, want second", info.InstanceID)
	}
	got, err := ReadLockForPath(dir)
	if err != nil || got.InstanceID != "second" {
		t.Fatalf("ReadLockForPath = %+v, %v; want instance second", got, err)
	}
}

// TestSameFileAtPathDetectsReplacedLockFile covers the guard against a lock
// file that was unlinked and recreated between open and flock.
func TestSameFileAtPathDetectsReplacedLockFile(t *testing.T) {
	path := lockFilePath(t.TempDir())
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !sameFileAtPath(f, path) {
		t.Fatal("freshly opened lock file not recognised as the file at path")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if sameFileAtPath(f, path) {
		t.Fatal("replaced lock file still recognised as the same inode")
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == os.PathSeparator {
			return path[:i]
		}
	}
	return "."
}
