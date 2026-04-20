package project

// CreatedEvent is published when a new project is registered.
type CreatedEvent struct {
	Project Project
}

// StatusChangedEvent is published when a project's status changes.
type StatusChangedEvent struct {
	ProjectID string
	OldStatus string
	NewStatus string
}

// SwitchedEvent is published when the active project changes.
type SwitchedEvent struct {
	ProjectID string // empty string means "back to main instance"
}

// DeletedEvent is published when a project is removed from the registry.
type DeletedEvent struct {
	ProjectID string
}

// InitRequiredEvent is published when activation is attempted on an uninitialized path.
type InitRequiredEvent struct {
	ProjectID string
	Path      string
}
