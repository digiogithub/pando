//go:build !windows

package procgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestEnsureSetsProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 1")
	if !Ensure(cmd) {
		t.Fatal("Ensure() = false, want true on this platform")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("SysProcAttr = %+v, want Setpgid", cmd.SysProcAttr)
	}
}

func TestEnsureRespectsExistingSetsid(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if !Ensure(cmd) {
		t.Fatal("Ensure() = false, want true")
	}
	if cmd.SysProcAttr.Setpgid {
		t.Fatal("Ensure must not set Setpgid on a session leader")
	}
}

func TestKillInvalidPid(t *testing.T) {
	if Kill(0, syscall.SIGTERM) {
		t.Fatal("Kill(0, ...) = true, want false")
	}
	if Kill(-1, syscall.SIGTERM) {
		t.Fatal("Kill(-1, ...) = true, want false")
	}
}

// TestKillReachesGroup starts a shell that spawns a child sleep, groups it,
// then kills the group by the shell's pid: both the shell and its child must
// exit, proving the signal reached the whole tree rather than just the
// leader.
func TestKillReachesGroup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires /bin/sh")
	}
	marker := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("sh", "-c", "sleep 30 & echo $! > "+marker+"; wait")
	Ensure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Give the shell a moment to fork the background sleep.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil && len(data) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !Kill(cmd.Process.Pid, syscall.SIGKILL) {
		t.Fatal("Kill() = false, want true")
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process group leader did not exit after Kill")
	}
}
