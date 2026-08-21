package extension

import (
	"context"
	"time"
)

// Memory capability: the hooks an extension uses to observe, and to augment,
// Pando's remembrance layer (persistent memories and knowledge-base
// documents).
//
// Two complementary shapes, on purpose:
//
//   - MemorySink is a write observer. It learns that something was written and
//     does whatever it wants with that fact, out of band. It cannot change what
//     core stored, and it cannot make a local write fail.
//   - RemembranceSearchWrapper is a read decorator. It sits in front of core's
//     search and may add, drop or reorder results — that is how a corporate
//     store's documents reach the agent alongside the local ones.
//
// Core owns these interfaces and the points that call them, and nothing else:
// transport, batching, retry, spooling, dedup and redaction all belong to the
// extension. That boundary is what keeps the exfiltration policy in one place
// instead of spread across core.
//
// # This capability moves project content off the machine
//
// It is off unless [Extensions.Memory] Enabled is true *and* the scope of the
// write is listed in Scopes. Core enforces that gate before a sink is ever
// called, so an extension cannot opt itself in. See internal/extensions.

// MemoryKind distinguishes the two kinds of record the remembrance layer
// holds. They live in the same table but mean different things: a memory is a
// short keyed fact with a TTL, a document is durable knowledge-base content.
type MemoryKind string

const (
	// KindMemory is a keyed memory (remember/recall).
	KindMemory MemoryKind = "memory"
	// KindDocument is a knowledge-base document.
	KindDocument MemoryKind = "document"
)

// MemoryOp is what happened to the record.
type MemoryOp string

const (
	MemoryCreated MemoryOp = "created"
	MemoryUpdated MemoryOp = "updated"
	MemoryDeleted MemoryOp = "deleted"
)

// MemoryOrigin says which part of Pando produced the write. A corporate sink
// needs it to tell a deliberate `remember` from a filesystem mirror sweep that
// re-imported ten thousand files: the first is worth pushing, the second is
// usually noise.
type MemoryOrigin string

const (
	// OriginTool is a write made by the agent through a tool (remember,
	// kb_add_document, ...). The default when nothing sets an origin.
	OriginTool MemoryOrigin = "tool"
	// OriginAPI is a write made through the REST API or the UI.
	OriginAPI MemoryOrigin = "api"
	// OriginSync is a bulk write from the knowledge-base filesystem mirror.
	OriginSync MemoryOrigin = "sync"
	// OriginWatcher is a write triggered by a filesystem change event.
	OriginWatcher MemoryOrigin = "watcher"
	// OriginGC is a write made by the memory garbage collector (expiry,
	// outdated flagging).
	OriginGC MemoryOrigin = "gc"
	// OriginRemote is a write that arrived from another instance or from a
	// remote store. A sink must not push it back out: that is how sync loops
	// are made.
	OriginRemote MemoryOrigin = "remote"
)

// MemoryEvent is one remembrance write.
//
// The field set is deliberately wide from the first release. Core and the
// enterprise module ship from two repositories, so adding a field later means
// a coordinated release of both; carrying a field nobody reads yet costs
// nothing.
type MemoryEvent struct {
	// Kind distinguishes a keyed memory from a KB document.
	Kind MemoryKind
	// Op is what happened.
	Op MemoryOp
	// Scope is the memory scope ("user/", "project/", "session/", ...). Empty
	// for documents that carry no scope. This is the field the per-scope
	// opt-in gate matches on.
	Scope string
	// Key is the memory key for keyed memories, empty otherwise.
	Key string
	// Path is the document path in the knowledge base. Always set: a keyed
	// memory is stored as a document too.
	Path string
	// Content is the record body. Empty on a delete.
	Content string
	// Tags are the record's tags.
	Tags []string
	// Embedding is the document-level embedding when core already computed
	// one, so a sink need not pay for it again. It may be nil even for a
	// content-bearing event, and a sink must cope with that.
	Embedding []float32
	// Metadata is the record's metadata map, JSON-decodable values only. It is
	// shared with other sinks: treat it as read-only.
	Metadata map[string]any
	// ProjectID identifies the project the write belongs to. Attribution, not
	// an isolation key: isolation belongs to the remote store.
	ProjectID string
	// UserID identifies the instance owner, when known.
	UserID string
	// InstanceID identifies this Pando instance, so a remote store can tell
	// two machines belonging to the same user apart.
	InstanceID string
	// Origin says which subsystem produced the write.
	Origin MemoryOrigin
	// Timestamp is when core observed the write.
	Timestamp time.Time
	// DryRun is true when the host is in dry-run mode. A sink must do
	// everything it normally does *except* send, and log what it would have
	// sent. Honouring this is a hard requirement, not a courtesy: dry-run is
	// how an operator audits what a corporate sink would exfiltrate.
	DryRun bool
}

// MemorySink observes remembrance writes.
//
// OnMemoryWrite is called with a context carrying the host's per-sink timeout.
// It must return promptly: real work (batching, HTTP, spooling) belongs on a
// queue the sink owns. Returning an error does not fail or roll back the local
// write — nothing an extension does can — it is only logged and counted.
//
// Delivery is best effort. When the host queue is full events are dropped, so
// a sink that must not lose events has to persist them itself, which is what
// the spool in the corporate sink is for.
type MemorySink interface {
	Extension
	// OnMemoryWrite receives one write. Never called with a nil event.
	OnMemoryWrite(ctx context.Context, ev MemoryEvent) error
}

// MemorySyncReporter is implemented by a sink that wants its state shown in
// the UI. The non-negotiable for this capability is that a user can always
// see, at a glance, that content leaves the machine and where it goes — so a
// sink that ships data and reports nothing is a bug.
type MemorySyncReporter interface {
	// MemorySyncStatus returns the sink's current state. Called from HTTP
	// handlers; it must not block.
	MemorySyncStatus() MemorySyncStatus
}

// MemorySyncStatus is what the UI shows about one sink.
type MemorySyncStatus struct {
	// Active reports whether the sink is currently shipping data.
	Active bool `json:"active"`
	// DryRun reports whether it is only pretending to.
	DryRun bool `json:"dryRun"`
	// Destination is human-readable and must identify where data goes (a host
	// name, not a secret-bearing URL).
	Destination string `json:"destination,omitempty"`
	// Scopes lists the scopes this sink is allowed to ship.
	Scopes []string `json:"scopes,omitempty"`
	// Pending is the number of events waiting to be sent.
	Pending int `json:"pending"`
	// Sent is the number of events shipped since start.
	Sent int64 `json:"sent"`
	// Dropped is the number of events lost, for any reason.
	Dropped int64 `json:"dropped"`
	// LastSyncAt is when the last successful send completed.
	LastSyncAt time.Time `json:"lastSyncAt,omitzero"`
	// LastError is the last failure, empty when healthy.
	LastError string `json:"lastError,omitempty"`
}

// RemembranceQuery is a search of the remembrance layer, in the neutral form a
// wrapper sees it.
type RemembranceQuery struct {
	// Query is the natural-language search text.
	Query string
	// Limit is the maximum number of results the caller wants.
	Limit int
	// Tags narrows the search to records carrying any of these tags.
	Tags []string
	// Scope narrows the search to one scope prefix. Empty means every scope.
	Scope string
	// ProjectID is the project the search runs in.
	ProjectID string
}

// Remembrance is one search hit in the neutral form.
type Remembrance struct {
	// Path identifies the record.
	Path string
	// Content is the matching text.
	Content string
	// Score is the relevance score, higher is better. Scores from different
	// stores are not comparable; a wrapper that merges two stores is
	// responsible for producing a ranking that makes sense.
	Score float64
	// Tags are the record's tags.
	Tags []string
	// Scope is the record's scope, when it has one.
	Scope string
	// Source names where the hit came from: empty (or "local") for core's own
	// store, otherwise a wrapper-supplied label shown to the user. A merged
	// result set that does not label its remote hits is indistinguishable from
	// a local one, which is exactly what the visibility requirement forbids.
	Source string
	// UpdatedAt is the record's last modification time, when known.
	UpdatedAt time.Time
}

// RemembranceSearcher is core's search, in the neutral form. A wrapper
// receives one and returns one.
type RemembranceSearcher interface {
	SearchRemembrances(ctx context.Context, q RemembranceQuery) ([]Remembrance, error)
}

// RemembranceSearcherFunc adapts a function to RemembranceSearcher.
type RemembranceSearcherFunc func(ctx context.Context, q RemembranceQuery) ([]Remembrance, error)

func (f RemembranceSearcherFunc) SearchRemembrances(ctx context.Context, q RemembranceQuery) ([]Remembrance, error) {
	return f(ctx, q)
}

// RemembranceSearchWrapper decorates remembrance search: the classic storage
// decorator, applied once at startup.
//
// The wrapper receives the next searcher in the chain and returns the one that
// replaces it. Chaining runs in registration order, so the first registered
// extension ends up outermost. A wrapper that decides not to act must return
// next unchanged.
//
// A wrapper that fails should fall back to next rather than return an error:
// a corporate store being unreachable must degrade to local-only search, never
// break the agent's ability to recall anything at all.
type RemembranceSearchWrapper interface {
	Extension
	// WrapRemembranceSearch is called once, during startup, before any search
	// runs. It must not block.
	WrapRemembranceSearch(next RemembranceSearcher) RemembranceSearcher
}
