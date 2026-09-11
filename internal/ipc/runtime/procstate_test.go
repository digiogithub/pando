// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package runtime

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
)

// TestIsSuspendedState pins the state letters that mean "stopped, resumable"
// (never kill) apart from the ones that mean "running but unresponsive" or
// "already gone" (kill is correct).
func TestIsSuspendedState(t *testing.T) {
	for _, state := range []byte{'T', 't'} {
		if !isSuspendedState(state) {
			t.Errorf("isSuspendedState(%q) = false, want true (a stopped process must never be killed)", string(state))
		}
		if suspendedStateReason(state) == "" {
			t.Errorf("suspendedStateReason(%q) is empty; the log line must say why", string(state))
		}
	}
	// R running, S sleeping, D uninterruptible I/O, Z zombie, X dead, I idle.
	for _, state := range []byte{'R', 'S', 'D', 'Z', 'X', 'I'} {
		if isSuspendedState(state) {
			t.Errorf("isSuspendedState(%q) = true, want false", string(state))
		}
	}
}

// TestProcessStateReportsRunningAndStopped drives the real /proc reader across
// a live child: sleeping, then SIGSTOPped, then resumed.
func TestProcessStateReportsRunningAndStopped(t *testing.T) {
	if !procStateSupported {
		t.Skip("processState is Linux-only (/proc/<pid>/stat)")
	}

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGCONT)
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	state, ok := processState(pid)
	if !ok {
		t.Fatalf("processState(%d) could not read the state of a live child", pid)
	}
	if isSuspendedState(state) {
		t.Fatalf("a freshly started `sleep` reports state %q, want a running/sleeping state", string(state))
	}

	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Skipf("cannot SIGSTOP the helper: %v", err)
	}
	if !waitForState(pid, isSuspendedState, 2*time.Second) {
		got, _ := processState(pid)
		t.Fatalf("after SIGSTOP the child reports state %q, want T/t", string(got))
	}

	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatalf("SIGCONT: %v", err)
	}
	if !waitForState(pid, func(s byte) bool { return !isSuspendedState(s) }, 2*time.Second) {
		got, _ := processState(pid)
		t.Fatalf("after SIGCONT the child still reports state %q, want a running/sleeping state", string(got))
	}
}

// TestProcessStateOnMissingProcess: a PID that cannot be inspected reports
// ok=false, so callers fall back to their platform-independent behaviour
// instead of treating "unknown" as "suspended".
func TestProcessStateOnMissingProcess(t *testing.T) {
	// PID 0 is never a readable /proc entry on Linux, and every non-Linux
	// build returns ok=false unconditionally.
	if _, ok := processState(0); ok {
		t.Fatal("processState(0) reported a known state")
	}
}

// TestKillStalePrimaryLeavesSuspendedPrimaryAlone is the G7 guard end-to-end:
// a primary that holds the lock, never answers ipc.ping, and is SIGSTOPped
// must NOT be killed — killStalePrimary returns false and the process is still
// alive (and still suspended) afterwards. Before G7 this process was
// SIGKILLed, which is exactly how an mcp-server spawned by an editor could
// destroy a TUI paused in a debugger.
func TestKillStalePrimaryLeavesSuspendedPrimaryAlone(t *testing.T) {
	if !procStateSupported {
		t.Skip("the suspended-primary guard needs /proc (Linux only)")
	}

	project := loadIsolatedConfig(t)
	// Deliberately ignoring the reap func: this process is SIGSTOPped below,
	// and a stopped process never exits, so Wait() would block forever. The
	// helper's own t.Cleanup kills it first (SIGKILL cannot be blocked or
	// deferred by a stop) and reaps it then.
	pid, _ := startFakeUnresponsivePrimary(t, project)

	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		t.Skipf("cannot SIGSTOP the fake primary: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGCONT) })
	if !waitForState(pid, isSuspendedState, 2*time.Second) {
		got, _ := processState(pid)
		t.Fatalf("the fake primary reports state %q, want it suspended before the probe", string(got))
	}

	info, err := ipc.ReadLockForPath(project)
	if err != nil || info == nil {
		t.Fatalf("ReadLockForPath: info=%v err=%v", info, err)
	}

	if killStalePrimary(context.Background(), project, info, 200*time.Millisecond) {
		t.Fatal("killStalePrimary killed a SUSPENDED primary; it must be left alone (G7)")
	}
	if !processAlive(pid) {
		t.Fatal("the suspended primary is gone; killStalePrimary must not have signalled it")
	}
	if state, ok := processState(pid); !ok || !isSuspendedState(state) {
		t.Fatalf("the primary is no longer suspended (state=%q ok=%v); it must be left exactly as it was", string(state), ok)
	}
}

// TestKillStalePrimaryStillKillsRunningUnresponsivePrimary is the control
// case: an unresponsive primary that is genuinely RUNNING (not suspended) is
// still killed, so G7 narrows the policy without disabling it.
func TestKillStalePrimaryStillKillsRunningUnresponsivePrimary(t *testing.T) {
	project := loadIsolatedConfig(t)
	pid, reap := startFakeUnresponsivePrimary(t, project)

	info, err := ipc.ReadLockForPath(project)
	if err != nil || info == nil {
		t.Fatalf("ReadLockForPath: info=%v err=%v", info, err)
	}

	if !killStalePrimary(context.Background(), project, info, 200*time.Millisecond) {
		t.Fatal("killStalePrimary left a running, unresponsive primary alive")
	}
	waitForProcessExit(pid, 2*time.Second)
	reap()
	if processAlive(pid) {
		t.Fatal("the unresponsive primary is still alive after killStalePrimary")
	}
}

// waitForState polls pid's state until pred accepts it or timeout elapses.
// The kernel applies a stop/continue asynchronously to the signalling call.
func waitForState(pid int, pred func(byte) bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if state, ok := processState(pid); ok && pred(state) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
