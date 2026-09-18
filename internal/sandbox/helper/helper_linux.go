package helper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// run confines the current thread and execs argv. It returns only on failure.
func run(spec Spec, argv []string) int {
	// Every restriction below is per thread; the thread that applies them
	// must be the one that calls execve.
	runtime.LockOSThread()

	path := spec.Path
	if path == "" {
		p, err := exec.LookPath(argv[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "pando sandbox: %v\n", err)
			return ExitNotFound
		}
		path = p
	}

	if err := confine(spec); err != nil {
		fmt.Fprintf(os.Stderr, "pando sandbox: %v\n", err)
		return ExitSetupFailed
	}

	err := unix.Exec(path, argv, os.Environ())
	// Only reached on failure.
	fmt.Fprintf(os.Stderr, "pando sandbox: exec %s: %v\n", path, err)
	if errors.Is(err, unix.ENOENT) {
		return ExitNotFound
	}
	return ExitSetupFailed
}

// confine applies no_new_privs, Landlock and seccomp to the calling thread.
func confine(spec Spec) error {
	arch, err := NativeArch()
	if err != nil {
		return err
	}
	filter, err := BuildFilter(arch, FilterOptions{
		RestrictNetwork:  spec.RestrictsNetwork(),
		AllowUnixSockets: spec.AllowUnixSockets,
	})
	if err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS): %w", err)
	}
	abi, err := LandlockABI()
	if err != nil {
		// The parent only wraps when its probe found Landlock; fail closed.
		return errors.New(LandlockUnavailableReason(err))
	}
	if err := applyLandlock(spec, abi); err != nil {
		return err
	}
	return installFilter(filter)
}

// installFilter loads the program with prctl(PR_SET_SECCOMP) on the calling
// thread (no TSYNC: see the Landlock note in landlock_linux.go).
func installFilter(filter []SockFilter) error {
	if len(filter) == 0 || len(filter) > 0xffff {
		return fmt.Errorf("invalid seccomp program length %d", len(filter))
	}
	raw := make([]unix.SockFilter, len(filter))
	for i, f := range filter {
		raw[i] = unix.SockFilter{Code: f.Code, Jt: f.Jt, Jf: f.Jf, K: f.K}
	}
	prog := unix.SockFprog{Len: uint16(len(raw)), Filter: &raw[0]}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER,
		uintptr(unsafe.Pointer(&prog)), 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_SECCOMP): %w", err)
	}
	runtime.KeepAlive(raw)
	return nil
}
