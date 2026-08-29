// Package events defines the backend-agnostic UI event subscription model
// for the Pando Desktop Controller. A platform backend (internal/uiauto/
// platform/...) may optionally implement Subscriber, in addition to
// core.Backend, to push live UI-tree changes instead of being polled;
// WaitFor prefers that live path and transparently falls back to
// core.WaitFor's polling loop for any backend that does not implement it,
// or when a live subscription itself fails. Capabilities.Events must
// reflect which is actually happening for a given backend/session -- never
// claim events support a backend cannot genuinely deliver.
package events

import (
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Kind enumerates the accessibility event categories a Subscriber can
// report. This is a deliberately small, backend-agnostic vocabulary; a
// backend maps its native event/signal names onto these.
type Kind string

const (
	// KindCreated signals a new element appeared (e.g. AT-SPI
	// ChildrenChanged:add, CDP DOM.childNodeInserted).
	KindCreated Kind = "created"
	// KindDestroyed signals an element disappeared/was removed.
	KindDestroyed Kind = "destroyed"
	// KindPropertyChanged signals a generic attribute/state change that
	// isn't more specifically a focus or value change.
	KindPropertyChanged Kind = "propertychanged"
	// KindFocusChanged signals keyboard focus moved.
	KindFocusChanged Kind = "focuschanged"
	// KindValueChanged signals an element's value/text content changed.
	KindValueChanged Kind = "valuechanged"
)

// Event is one backend-reported UI change.
type Event struct {
	Kind Kind
	// ElementRef is a best-effort qualified reference to the affected
	// element, when the backend can map the raw native event onto a live
	// Manager snapshot ref. It is frequently empty: events are a wake-up
	// signal, not a source of truth -- WaitFor always re-evaluates the
	// actual condition against the backend rather than trusting this
	// field, so an empty ElementRef never blocks correctness.
	ElementRef core.ElementRef
	// AppID/WindowID identify the owning application/window when the
	// backend can cheaply provide them.
	AppID    string
	WindowID string
	// Timestamp is when this package observed the event (not necessarily
	// when the backend/OS generated it).
	Timestamp time.Time
	// Details carries whatever raw, backend-specific information is
	// useful for diagnostics (e.g. the native signal member name, the CDP
	// node id). Not part of the stable cross-backend contract.
	Details map[string]any
}
