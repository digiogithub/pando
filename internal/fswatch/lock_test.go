//go:build !windows

package fswatch

import (
	"path/filepath"
	"testing"
)

func TestTryLockExclusiveAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	rel, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("first lock: ok=%v err=%v", ok, err)
	}
	if _, ok2, err := TryLock(path); err != nil || ok2 {
		t.Fatalf("second lock must fail: ok=%v err=%v", ok2, err)
	}
	rel()
	rel() // idempotent
	rel3, ok3, err := TryLock(path)
	if err != nil || !ok3 {
		t.Fatalf("re-acquire: ok=%v err=%v", ok3, err)
	}
	rel3()
}

func TestTryLockDirCreatesDir(t *testing.T) {
	dir := t.TempDir()
	rel, ok, err := TryLockDir(dir)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	rel()
}
