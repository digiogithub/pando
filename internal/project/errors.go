package project

import "errors"

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
