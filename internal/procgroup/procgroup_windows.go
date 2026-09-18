//go:build windows

package procgroup

import (
	"os/exec"
	"syscall"
)

// Ensure is a no-op on Windows: there is no POSIX process-group concept, and
// tree containment there is a Job Object attached after Start (see
// internal/sandbox.AttachProcessTree), not process grouping.
func Ensure(*exec.Cmd) bool { return false }

// Kill is unsupported on Windows; callers fall back to signalling the
// process itself.
func Kill(int, syscall.Signal) bool { return false }
