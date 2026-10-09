//go:build !windows

package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/fswatch"
)

func TestRunWithCodeWatchLockTakeover(t *testing.T) {
	dir := t.TempDir()
	release, ok, err := fswatch.TryLockDir(dir)
	if err != nil || !ok {
		t.Fatalf("setup lock: ok=%v err=%v", ok, err)
	}

	var ran atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWithCodeWatchLock(context.Background(), dir, 10*time.Millisecond, func() { ran.Add(1) })
	}()

	time.Sleep(60 * time.Millisecond)
	if ran.Load() != 0 {
		t.Fatal("fn ran while lock held elsewhere")
	}
	release()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not take over after release")
	}
	if ran.Load() != 1 {
		t.Fatalf("fn ran %d times", ran.Load())
	}
}

func TestRunWithCodeWatchLockCancel(t *testing.T) {
	dir := t.TempDir()
	release, _, _ := fswatch.TryLockDir(dir)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWithCodeWatchLock(ctx, dir, 10*time.Millisecond, func() { t.Error("must not run") })
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not exit on cancel")
	}
}
