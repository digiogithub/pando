// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package runtime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
)

// startFakeUnresponsivePrimary seeds workdir's IPC lock file with a real,
// alive process that holds a real OS-level flock on it (so killing that
// process actually releases the lock, exactly like a killed real primary)
// but never answers ipc.ping, because nothing listens on the port recorded
// for it. The lock is held by a `flock(1)` child: it opens and flocks the
// path, then execs into `sleep 30` while keeping the locked fd open across
// the exec — so the one PID both holds the lock and is safely killable. The
// LockInfo JSON naming that PID is written afterwards by this (unlocked)
// process: flock() is advisory and does not block plain read/write syscalls
// from other processes, only other flock() attempts.
//
// Not using ipc.AcquireLock here: it always records the CALLING process's own
// PID, but this test needs a different, alive, killable PID (and
// killStalePrimary's self/PID-0 safety guards would otherwise mask the test).
//
// Returns the PID and a reap func: call it after anything might have killed
// the process (e.g. Bootstrap's own killStalePrimary) and before checking
// liveness — a killed-but-unreaped child is a zombie, and a zombie still
// answers the signal(pid, 0) probe processAlive uses, which would make the
// process look alive even though it is fully gone and the lock (released
// immediately on kill, independent of reaping) is free. reap is idempotent
// (a second Wait after the first just errors, which it ignores) and is also
// registered via t.Cleanup so tests that never call it directly still reap.
func startFakeUnresponsivePrimary(t *testing.T, workdir string) (pid int, reap func()) {
	t.Helper()
	canon := canonicalWorkdir(workdir)
	pubPort, rpcPort := ipc.PortsForPath(canon)

	pandoDir := filepath.Join(canon, ".pando")
	if err := os.MkdirAll(pandoDir, 0o700); err != nil {
		t.Fatalf("mkdir .pando: %v", err)
	}
	lockPath := filepath.Join(pandoDir, "ipc.lock")

	// --no-fork makes flock(1) exec-replace itself with "sleep 30" (keeping
	// the same PID) instead of forking a child to run it and waiting: the
	// locked fd is then held by the one PID we can kill, and the kernel
	// releases the flock as soon as that PID dies. Without it, flock(1) forks
	// and keeps running as the parent while the lock is actually held by an
	// untracked grandchild, so killing cmd.Process.Pid alone never frees it.
	cmd := exec.Command("flock", "--exclusive", "--no-fork", lockPath, "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start flock(1) helper process: %v", err)
	}
	pid = cmd.Process.Pid
	reap = func() { _, _ = cmd.Process.Wait() }
	t.Cleanup(func() { _ = cmd.Process.Kill(); reap() })

	deadline := time.Now().Add(2 * time.Second)
	for !pathIsLocked(t, lockPath) {
		if time.Now().After(deadline) {
			t.Fatalf("flock(1) helper never acquired the lock on %s", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}

	info := ipc.LockInfo{
		InstanceID: "fake-stale-primary",
		PID:        pid,
		PubPort:    pubPort,
		RPCPort:    rpcPort,
		StartedAt:  time.Now().UTC(),
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal lock info: %v", err)
	}
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open lock file for writing: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write lock info: %v", err)
	}
	return pid, reap
}

// pathIsLocked reports whether some other process currently holds an
// exclusive flock on path, by trying (and immediately releasing) a
// non-blocking flock of our own.
func pathIsLocked(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// TestBootstrapWithOptionsDoesNotKillDisallowedStalePrimary covers the P1
// no-kill policy: with AllowKillStalePrimary=false, an unresponsive primary
// must be left running — this instance continues as a (degraded) secondary
// instead of SIGKILLing it. Uses a fake unresponsive lock holder; never kills
// a real process outside this test's own helper.
func TestBootstrapWithOptionsDoesNotKillDisallowedStalePrimary(t *testing.T) {
	project := loadIsolatedConfig(t)
	fakePID, _ := startFakeUnresponsivePrimary(t, project)

	res, err := BootstrapWithOptions(context.Background(), project, "secondary-no-kill", Options{
		ProbeTimeout:          200 * time.Millisecond,
		AllowKillStalePrimary: false,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions: %v", err)
	}
	t.Cleanup(res.Cleanup)

	if res.Role != RoleSecondary {
		t.Fatalf("role = %s, want %s", res.Role, RoleSecondary)
	}
	if !processAlive(fakePID) {
		t.Fatal("BootstrapWithOptions killed the stale primary despite AllowKillStalePrimary=false")
	}
}

// TestBootstrapWithOptionsKillsStalePrimaryWhenAllowed is the control case:
// the same unresponsive primary, but with AllowKillStalePrimary=true (what
// Bootstrap/DefaultOptions use today), must still be killed so this instance
// takes over as primary.
func TestBootstrapWithOptionsKillsStalePrimaryWhenAllowed(t *testing.T) {
	project := loadIsolatedConfig(t)
	fakePID, reap := startFakeUnresponsivePrimary(t, project)

	res, err := BootstrapWithOptions(context.Background(), project, "secondary-kill-allowed", Options{
		ProbeTimeout:          200 * time.Millisecond,
		AllowKillStalePrimary: true,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions: %v", err)
	}
	t.Cleanup(res.Cleanup)

	if res.Role != RolePrimary {
		t.Fatalf("role = %s, want %s (the stale primary should have been killed and the lock re-acquired)", res.Role, RolePrimary)
	}
	waitForProcessExit(fakePID, 2*time.Second)
	reap()
	if processAlive(fakePID) {
		t.Fatal("stale primary is still alive; AllowKillStalePrimary=true should have killed it")
	}
}

// TestBootstrapWithOptionsHonoursProbeTimeout checks that a custom
// ProbeTimeout is actually used for the stale-primary probe, not the 10s
// stalePrimaryProbeTimeout default — otherwise a short-lived, low-trust
// entrypoint like P2's mcp-server would block for 10s on every unresponsive
// primary regardless of the option it passed.
func TestBootstrapWithOptionsHonoursProbeTimeout(t *testing.T) {
	project := loadIsolatedConfig(t)
	_, _ = startFakeUnresponsivePrimary(t, project)

	const probeTimeout = 150 * time.Millisecond
	start := time.Now()
	res, err := BootstrapWithOptions(context.Background(), project, "secondary-probe-timeout", Options{
		ProbeTimeout:          probeTimeout,
		AllowKillStalePrimary: false,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("BootstrapWithOptions: %v", err)
	}
	t.Cleanup(res.Cleanup)

	if elapsed < probeTimeout {
		t.Fatalf("returned in %v, before the configured probe timeout (%v) elapsed", elapsed, probeTimeout)
	}
	// Well under the old 10s default: proves the configured timeout, not
	// stalePrimaryProbeTimeout, was honoured.
	if elapsed > 3*time.Second {
		t.Fatalf("took %v; the configured probe timeout does not appear to have been honoured", elapsed)
	}
}
