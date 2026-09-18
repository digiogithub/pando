//go:build !windows

package procgroup

import (
	"os/exec"
	"syscall"
)

// Ensure makes cmd the leader of its own process group before Start, unless
// it is already going to be one (Setsid was requested by the caller or a
// sandbox Wrapper — setpgid fails for a session leader). It always returns
// true on this platform: after Start, Kill(cmd.Process.Pid, ...) reaches the
// whole group.
func Ensure(cmd *exec.Cmd) bool {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.Setsid {
		return true
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
	return true
}

// Kill signals the process group led by pid. It reports whether the group
// could be signalled at all (a false return means the caller should fall
// back to signalling the process alone).
func Kill(pid int, sig syscall.Signal) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(-pid, sig) == nil
}
