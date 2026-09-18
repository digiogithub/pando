//go:build !windows

package sandbox

import "os"

// AttachProcessTree is a no-op on every OS but Windows: the Linux and macOS
// backends already confine and clean up their own children through their
// Wrapper (Landlock/seccomp, sandbox-exec), so there is nothing extra to
// attach here. See jobobject_windows.go for the real implementation.
//
// release is always non-nil and safe to call any number of times.
func AttachProcessTree(p *os.Process) (release func(), err error) {
	return noopRelease, nil
}

func noopRelease() {}
