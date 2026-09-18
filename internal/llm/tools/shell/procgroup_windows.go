//go:build windows

package shell

import "os/exec"

// setProcessGroup is a no-op on Windows: the shell's tree is contained by the
// Job Object attached after Start (sandbox.AttachProcessTree).
func setProcessGroup(*exec.Cmd) {}

// killProcessGroup is unsupported on Windows; callers fall back to killing
// the process itself.
func killProcessGroup(int, <-chan struct{}) bool { return false }
