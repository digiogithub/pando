package runtime

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc"
)

func TestProcessAlive(t *testing.T) {
	// A long-lived child process is reliably alive until we kill it.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	if !processAlive(pid) {
		t.Fatalf("processAlive(%d) = false, want true for a running child", pid)
	}

	if err := killProcess(pid); err != nil {
		t.Fatalf("killProcess(%d) error: %v", pid, err)
	}
	_, _ = cmd.Process.Wait() // reap so the PID is fully released

	waitForProcessExit(pid, 2*time.Second)
	if processAlive(pid) {
		t.Fatalf("processAlive(%d) = true after kill, want false", pid)
	}
}

// TestKillStalePrimaryNeverTargetsSelf guards the safety check that prevents an
// instance from killing its own process when the lock file (stale or racy)
// happens to record this PID.
func TestKillStalePrimaryNeverTargetsSelf(t *testing.T) {
	self := &ipc.LockInfo{PID: os.Getpid()}

	if killStalePrimary(context.Background(), t.TempDir(), self) {
		t.Fatal("killStalePrimary targeted our own PID; must never kill self")
	}
}

// TestKillStalePrimaryIgnoresInvalidPID ensures we do not act on a malformed lock.
func TestKillStalePrimaryIgnoresInvalidPID(t *testing.T) {
	if killStalePrimary(context.Background(), t.TempDir(), &ipc.LockInfo{PID: 0}) {
		t.Fatal("killStalePrimary acted on PID 0")
	}
	if killStalePrimary(context.Background(), t.TempDir(), nil) {
		t.Fatal("killStalePrimary acted on nil lockInfo")
	}
}
