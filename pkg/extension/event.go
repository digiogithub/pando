package extension

import (
	"context"
	"time"
)

// Core publishes lifecycle events for its resources. An extension can observe
// them without knowing anything about core's internal types: the payload is the
// resource in its JSON form, which is the same shape the REST API already
// exposes and is therefore already a public surface.

// EventType is what happened to a resource.
type EventType string

const (
	EventCreated EventType = "created"
	EventUpdated EventType = "updated"
	EventDeleted EventType = "deleted"
)

// The topics core publishes. More may be added; an unknown topic is not an
// error, and a subscriber must ignore what it does not recognise.
const (
	TopicSession    = "session"
	TopicMessage    = "message"
	TopicPermission = "permission"
)

// Event is one resource lifecycle notification.
type Event struct {
	// Topic identifies the resource kind (see the Topic constants).
	Topic string
	// Type is what happened.
	Type EventType
	// ID is the resource identifier when the payload carries one.
	ID string
	// SessionID is the session the resource belongs to, when it belongs to one.
	SessionID string
	// Payload is the resource as JSON-decoded values: strings, float64 numbers,
	// bools, maps and slices. One map is shared by every subscriber of the
	// event, so treat it as read-only and copy anything you keep past the call.
	Payload map[string]any
	// Time is when the host observed the event.
	Time time.Time
}

// EventSubscriber is implemented by extensions that observe resource lifecycle
// events. This is how a corporate memory sink learns that a session ended or a
// message was written without core knowing anything about it.
type EventSubscriber interface {
	Extension
	// Topics lists the topics to receive. An empty result means every topic,
	// including topics added in later versions.
	Topics() []string
	// HandleEvent is called from the host's fan-out goroutine and must return
	// promptly: slow work belongs on a queue the extension owns. Events are
	// dropped rather than queued when a subscriber cannot keep up, so an
	// extension that must not lose events has to buffer them itself.
	HandleEvent(ctx context.Context, ev Event)
}
