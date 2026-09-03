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

	// EventOverlayApplied is the Type of a TopicConfig event published after a
	// load in which a configuration overlay was merged. Payload["changedKeys"]
	// names the paths whose value the overlay moved.
	EventOverlayApplied EventType = "overlay_applied"

	// EventConfigReloaded is the Type of a TopicConfig event published after
	// the host reloaded its configuration, whatever caused it: a file change,
	// a settings save, an extension asking for one.
	EventConfigReloaded EventType = "config_reloaded"
)

// The topics core publishes. More may be added; an unknown topic is not an
// error, and a subscriber must ignore what it does not recognise.
const (
	TopicSession    = "session"
	TopicMessage    = "message"
	TopicPermission = "permission"

	// TopicConfig carries host configuration changes. Unlike the resource
	// topics its events are not about one identified object: ID and SessionID
	// are empty, Type says what happened (EventUpdated for an ordinary change,
	// EventOverlayApplied, EventConfigReloaded) and the payload carries
	//
	//	"event"       string   the host's own name for the change, if any
	//	"section"     string   which part of the configuration moved, or ""
	//	"source"      string   where the change came from ("file", "tui",
	//	                       "webui", "overlay", "reload")
	//	"changedKeys" []any    dotted paths whose value changed, when the
	//	                       publisher can name them; absent means unknown,
	//	                       so assume the whole section moved
	//	"lockedKeys"  []any    the lock list as it stands after the change
	//	"timestamp"   string   RFC 3339
	//
	// This is how an extension that imposes configuration learns that its
	// values went live, and how it notices a local edit fighting its overlay.
	TopicConfig = "config"
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
