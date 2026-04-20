package project

import "errors"

// ErrProjectNeedsInit is returned when Activate is called on a project path
// that has no .pando.toml (or .pando.json) configuration file.
// The caller should guide the user through the init flow before retrying.
var ErrProjectNeedsInit = errors.New("project needs initialization: no .pando.toml found at path")
