package project_test

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy/proxytest"
	"github.com/digiogithub/pando/internal/project"
)

// On an IPC secondary the project service writes through the DBProxy: every
// write (including the hand-written rename) succeeds while the primary holds
// the write lock longer than the secondary's 200 ms busy timeout.
func TestProjectServiceWritesThroughProxyOnSecondary(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	svc := project.NewService(tp.Proxy)
	dir := t.TempDir()

	tp.HoldWriteLock(t, 700*time.Millisecond)
	started := time.Now()
	p, err := svc.Create(ctx, "p5", dir)
	if err != nil {
		t.Fatalf("Create under primary lock: %v", err)
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatalf("Create returned in %s while the lock was held (not forwarded?)", time.Since(started))
	}

	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := svc.Rename(ctx, p.ID, "renamed"); err != nil {
		t.Fatalf("Rename under primary lock: %v", err)
	}
	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := svc.UpdateStatus(ctx, p.ID, project.StatusRunning, 42, 4242); err != nil {
		t.Fatalf("UpdateStatus under primary lock: %v", err)
	}
	if err := svc.TouchLastOpened(ctx, p.ID); err != nil {
		t.Fatalf("TouchLastOpened: %v", err)
	}

	got, err := svc.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.Status != project.StatusRunning || got.ACPPID != 42 || got.LastOpened == nil {
		t.Fatalf("unexpected project after proxied writes: %+v", got)
	}

	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := svc.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete under primary lock: %v", err)
	}
	if _, err := svc.Get(ctx, p.ID); err == nil {
		t.Fatal("project still present after proxied delete")
	}
}
