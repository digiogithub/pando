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

	// EventStarted and EventCompleted bracket one activity that takes time.
	// They are always published as a pair on the same topic and with the same
	// ID, so a subscriber can measure or correlate them; EventCompleted also
	// carries the duration, so a subscriber that only wants the outcome may
	// ignore EventStarted entirely.
	EventStarted   EventType = "started"
	EventCompleted EventType = "completed"

	// EventConnected and EventFailed report the outcome of establishing a
	// connection to an external service.
	EventConnected EventType = "connected"
	EventFailed    EventType = "failed"

	// EventAuthRequired is the Type of an event reporting that an external
	// service refused the host for want of credentials, so a human has to
	// authorise it before it can be used.
	EventAuthRequired EventType = "auth_required"

	// EventActivated is the Type of an event reporting that an optional piece
	// of behaviour was switched on for a run.
	EventActivated EventType = "activated"
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

	// TopicTool carries one tool execution. Two events are published per call,
	// EventStarted when the host hands the call to the tool and EventCompleted
	// when the tool returns, both with the tool call id as ID. Payload:
	//
	//	"name"       string   the tool name the model asked for
	//	"agent"      string   the agent that is running the call
	//	"durationMs" float64  wall time of the call (EventCompleted only)
	//	"ok"         bool     whether the call succeeded (EventCompleted only)
	//	"error"      string   failure reason, absent when ok is true
	//
	// Arguments and results are deliberately absent: they are the user's
	// content, and a topic meant for accounting must not become a copy of the
	// conversation. An extension that needs them uses the tool capability,
	// where interception is explicit and visible to the user.
	TopicTool = "tool"

	// TopicMCP carries the outcome of an MCP server handshake, with the server
	// name as ID. Type is EventConnected, EventAuthRequired when the server
	// refused the host for want of credentials, or EventFailed. Payload:
	//
	//	"server"    string  the configured server name
	//	"transport" string  "stdio", "sse" or "http"
	//	"client"    string  which part of the host connected
	//	"error"     string  failure reason, absent on EventConnected
	//
	// No credential material is ever put in the payload.
	TopicMCP = "mcp"

	// TopicSkill reports that a skill was activated for a run, with the skill
	// name as ID and EventActivated as Type. Payload:
	//
	//	"name"   string  the skill name
	//	"agent"  string  the agent the skill was activated for
	//	"source" string  what activated it ("auto" for prompt matching)
	//
	// Skill instructions are not carried: the name is what an inventory or a
	// usage report needs, and the body may be private to the user.
	TopicSkill = "skill"

	// TopicProvider reports a change to the configured provider accounts, with
	// the account id as ID and EventCreated, EventUpdated or EventDeleted as
	// Type. Payload:
	//
	//	"id"           string  the account id
	//	"providerType" string  the provider the account belongs to
	//	"disabled"     bool    whether the account is currently disabled
	//
	// API keys, tokens and base URLs are never published: an observer is told
	// that the set of accounts moved, not what is in them.
	TopicProvider = "provider"
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
