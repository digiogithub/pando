package project

import "time"

// Status constants for project lifecycle states.
const (
	StatusStopped      = "stopped"
	StatusRunning      = "running"
	StatusError        = "error"
	StatusInitializing = "initializing"
	StatusMissing      = "missing" // path no longer exists on disk
)

// Project represents a registered project directory managed by Pando.
type Project struct {
	ID          string
	Name        string
	Path        string
	Status      string
	Initialized bool
	// External is a computed (non-persisted) flag: true when the project is
	// running but its ACP instance was launched by another application (e.g. an
	// editor in ACP mode) rather than by this manager, so it cannot be stopped
	// from here. It is populated on demand via Manager.Runtime and is zero in
	// values returned directly from the database.
	External   bool
	ACPPID     int
	ACPPort    int
	LastOpened *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
