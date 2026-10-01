package project

import (
	"errors"
	"strings"
)

// ErrProjectNeedsInit is returned when Activate is called on a project path
// that has no .pando.toml (or .pando.json) configuration file.
// The caller should guide the user through the init flow before retrying.
var ErrProjectNeedsInit = errors.New("project needs initialization: no .pando.toml found at path")

// ErrExternalInstance is returned by Stop when the project is running but its
// ACP instance was launched by another application (e.g. an editor's ACP
// integration) rather than by this manager. Such an instance cannot be stopped
// from here; the user must close it from the application that started it.
var ErrExternalInstance = errors.New("project instance was launched externally and cannot be stopped from here")

// ErrInstanceNotRunning is returned by Delegate when no manager-owned child ACP
// instance is currently running for the requested project. Routing the work to a
// warm instance is only possible while one is live; callers that want auto-start
// must spawn the instance first (see the warm-target routing phase).
var ErrInstanceNotRunning = errors.New("no running manager-owned instance for project")

// ErrWarmCapReached is returned by WarmDelegate when the per-instance concurrency
// cap (Delegation.MaxConcurrent) is already reached. The caller should fall back
// to the cold path rather than overloading a single warm instance.
var ErrWarmCapReached = errors.New("warm instance concurrency cap reached")

// ErrProjectNotRegistered is returned by WarmDelegate when neither a project id
// nor a resolvable project path maps to a registered project, so no warm target
// can be selected. The caller should take the cold path.
var ErrProjectNotRegistered = errors.New("project is not registered")

// ErrExternalDelegationRefused is returned by DelegateExternal when the
// external instance is reachable over IPC but has opted out of accepting
// delegations (AcceptsDelegations=false or DelegationProtocol too old).
var ErrExternalDelegationRefused = errors.New("external instance does not accept delegations")

// ErrExternalUnreachable is returned by DelegateExternal when the external
// instance cannot be contacted over IPC: the lock file is absent, the PID is
// dead, or the ZeroMQ call fails.
var ErrExternalUnreachable = errors.New("external instance is unreachable over IPC")

// ErrSelfInstance is returned by EnsureInstance when the project is served by
// THIS very process (its on-disk IPC lock carries our own PID). Delegating there
// would send an IPC delegation.run request back to ourselves — running the
// subagent as a hidden session inside the parent instance instead of a separate
// agent loop — so warm routing refuses it and the caller takes the cold path.
var ErrSelfInstance = errors.New("project is served by this instance; warm delegation to self is not allowed")

// ErrChildStartupTimeout is returned when a spawned background child never
// becomes healthy within the startup timeout window.
var ErrChildStartupTimeout = errors.New("child startup timed out")

// ErrChildStartupFailed is returned when a background project child cannot be
// started or never reaches a healthy ready state.
var ErrChildStartupFailed = errors.New("child startup failed")

// ChildStartupError carries a startup failure detail suitable for API
// responses and logs. Detail never includes the child API token because the
// token is only learned after a successful startup handshake.
type ChildStartupError struct {
	Detail string
	Cause  error
}

func (e *ChildStartupError) Error() string {
	detail := strings.TrimSpace(e.Detail)
	switch {
	case detail == "" && e.Cause != nil:
		return e.Cause.Error()
	case detail == "":
		return ErrChildStartupFailed.Error()
	default:
		return detail
	}
}

func (e *ChildStartupError) Unwrap() error {
	if e == nil || e.Cause == nil {
		return ErrChildStartupFailed
	}
	return e.Cause
}

func (e *ChildStartupError) Is(target error) bool {
	return target == ErrChildStartupFailed || errors.Is(e.Cause, target)
}

// ErrChildInstance is returned when a project-child Pando instance is asked to
// spawn or initialize another project child. Child instances may delegate to
// external peers over IPC, but they must never create recursive descendants.
var ErrChildInstance = errors.New("project child instances cannot spawn nested project instances")
