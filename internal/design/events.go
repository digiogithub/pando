package design

import (
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/pubsub"
)

// Event kinds published by the design subsystem. They are the names the SSE
// stream carries, so surfaces can switch on them directly.
const (
	// EventCreated fires once, when an artifact directory is materialised.
	EventCreated = "design.created"
	// EventVersion fires when an iteration is committed, which is what a
	// version timeline and a thumbnail strip listen for.
	EventVersion = "design.version"
	// EventRender fires after a successful render, carrying the fresh node
	// count so an open preview knows to reload.
	EventRender = "design.render"
	// EventCritique fires when a critic pass scores a version (P8).
	EventCritique = "design.critique"
)

// Event is one design lifecycle notification. It is deliberately flat and
// small: it travels over SSE to every connected surface, and a surface that
// wants more calls the REST API for it.
type Event struct {
	Kind         string `json:"kind"`
	ArtifactID   string `json:"artifact_id"`
	SessionID    string `json:"session_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Slug         string `json:"slug,omitempty"`
	ArtifactKind Kind   `json:"artifact_kind,omitempty"`
	Version      int    `json:"version,omitempty"`
	Summary      string `json:"summary,omitempty"`
	// URL is the preview address when one could be minted, empty otherwise.
	URL string `json:"url,omitempty"`
	// Nodes is the size of the index a render produced.
	Nodes int `json:"nodes,omitempty"`
	// Slides is the deck slide count.
	Slides int `json:"slides,omitempty"`
	// Score is the critic score, for EventCritique.
	Score float64   `json:"score,omitempty"`
	At    time.Time `json:"at"`
}

var (
	eventsOnce   sync.Once
	eventsBroker *pubsub.Broker[Event]
)

// Events returns the process-wide design event broker. It is created on first
// use so a process that never designs anything never allocates it.
func Events() *pubsub.Broker[Event] {
	eventsOnce.Do(func() { eventsBroker = pubsub.NewBroker[Event]() })
	return eventsBroker
}

// publish emits an event, filling the timestamp and the session. It never
// blocks the caller: the broker drops to slow subscribers rather than stalling
// an agent turn behind a browser tab that stopped reading.
func (s *Service) publish(kind string, event Event) {
	event.Kind = kind
	if event.SessionID == "" {
		event.SessionID = s.sessionID
	}
	event.At = time.Now()
	if (kind == EventRender || kind == EventVersion) && event.ArtifactID != "" {
		if server := PreviewServer(); server != nil {
			server.Bump(event.ArtifactID)
		}
	}
	Events().Publish(pubsub.EventType(kind), event)
}
