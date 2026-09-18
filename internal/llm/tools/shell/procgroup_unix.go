//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup starts the shell as the leader of its own process group so
// killProcessGroup reaches every process it spawns, including those below a
// sandbox launcher. The shell reads commands from a pipe (no controlling
// terminal), so leaving Pando's group has no job-control side effects. When a
// wrapper already asked for a new session (Setsid), the process is a group
// leader anyway and Setpgid must not be added (setpgid fails for a session
// leader).
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.Setsid {
		return
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

// killProcessGroup sends SIGTERM to the process group led by pid and, when
// the leader has not exited (done not closed) after a short grace period,
// SIGKILL. It reports whether the group could be signalled.
func killProcessGroup(pid int, done <-chan struct{}) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return false
	}
	go func() {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
	return true
}
