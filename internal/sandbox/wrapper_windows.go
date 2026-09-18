package sandbox

import (
	"os/exec"
	"sync"
)

// platformWrapper returns the Windows backend.
//
// Windows has no unprivileged filesystem sandbox comparable to
// Landlock/Seatbelt: AppContainer needs ACL grants on the workspace and
// breaks many dev tools, and a restricted-token/low-integrity approach can't
// write the medium-integrity workspace without relabeling it (a later spike
// may revisit AppContainer; see the epic notes). So this backend never
// confines a command — Capability.Enforced is always false, and Wrap leaves
// cmd untouched, per the Wrapper contract (sandbox.go): the sandbox fails
// open and permission prompts stay on (Active/AutoAllowBash both gate on
// Capability.Enforced, so no separate override is needed here).
//
// What Windows does get is process-tree cleanup: AttachProcessTree
// (jobobject_windows.go) puts a started child in a Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so the whole tree dies when the handle
// is closed, including when Pando itself is killed. That is containment,
// not a sandbox, so it does not affect Capability.Enforced; it is called by
// the shell integration (PANDO-US-0044) after Start, not from Wrap.
func platformWrapper() Wrapper {
	return windowsWrapper{}
}

type windowsWrapper struct{}

// windowsReason is the Capability.Reason reported on Windows, whether or not
// Job Object containment is available: the backend is never enforced.
const windowsReason = "filesystem sandbox not supported on windows"

// windowsCapability probes once (CreateJobObject) and caches the result:
// Job Objects have existed since Windows 2000 and nested jobs work by
// default since Windows 8, so this should always succeed on a real Windows
// machine, but a hardened/nested environment that refuses even that
// degrades to Backend:"none" instead of claiming containment it cannot do.
var windowsCapability = sync.OnceValue(func() Capability {
	backend := BackendNone
	if jobObjectsSupported() {
		backend = BackendJobObject
	}
	return Capability{Backend: backend, Enforced: false, Reason: windowsReason}
})

func (windowsWrapper) Capability() Capability {
	return windowsCapability()
}

// Wrap never confines cmd: see platformWrapper. It always returns nil so
// callers fail open instead of refusing to run the command.
func (windowsWrapper) Wrap(*exec.Cmd, Policy) error {
	return nil
}
