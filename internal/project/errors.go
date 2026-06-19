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
