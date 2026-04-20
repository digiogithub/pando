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
	ACPPID      int
	ACPPort     int
	LastOpened  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
