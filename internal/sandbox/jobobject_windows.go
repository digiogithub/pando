package sandbox

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AttachProcessTree puts p in a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: the whole process tree is killed the
// moment the last handle to the job is closed, whether that happens because
// release runs or because Pando itself is killed and the OS reclaims its
// handles. This is containment, not a sandbox — it does not restrict what p
// can read, write or reach on the network (see platformWrapper in
// wrapper_windows.go for why Windows has no such backend).
//
// The shell integration (PANDO-US-0044) calls AttachProcessTree once, right
// after starting the persistent shell, so a crashed or force-killed Pando
// still cleans up whatever that shell spawned.
//
// release is always non-nil and safe to call any number of times (only the
// first call closes the handle). Callers are not required to call it: on
// process exit the OS closes every handle Pando held, which triggers the
// same kill.
func AttachProcessTree(p *os.Process) (release func(), err error) {
	if p == nil {
		return noopRelease, fmt.Errorf("sandbox: AttachProcessTree: nil process")
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return noopRelease, fmt.Errorf("sandbox: CreateJobObject: %w", err)
	}
	closeJob := func() { _ = windows.CloseHandle(job) }

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		closeJob()
		return noopRelease, fmt.Errorf("sandbox: SetInformationJobObject: %w", err)
	}

	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		closeJob()
		return noopRelease, fmt.Errorf("sandbox: OpenProcess(%d): %w", p.Pid, err)
	}
	defer windows.CloseHandle(proc)

	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		closeJob()
		return noopRelease, fmt.Errorf("sandbox: AssignProcessToJobObject(%d): %w", p.Pid, err)
	}

	var once sync.Once
	return func() { once.Do(closeJob) }, nil
}

func noopRelease() {}

// jobObjectsSupported probes whether this process can create Job Objects at
// all, for Capability reporting (windowsCapability in wrapper_windows.go).
// Job Objects have existed since Windows 2000 and nested jobs work by
// default since Windows 8, so this should always succeed on a real machine;
// it exists so an unusually restrictive environment degrades to
// Backend:"none" instead of silently claiming containment it cannot do.
func jobObjectsSupported() bool {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(job)
	return true
}
