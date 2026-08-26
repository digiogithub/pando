// Package design implements Pando's Design Studio: HTML/CSS/JS design
// artifacts that agents build, render, inspect and iterate on.
//
// An artifact is a directory in the user's working tree (designer/<slug>/ by
// default) holding an entry document, its assets and a portable
// pando-design.json manifest. Agents mutate those files with the regular
// write/edit tools, so permissions, diffs and agent-vcs keep working unchanged.
//
// History is not a copy-per-version store: each accepted iteration takes a
// directory-scoped snapshot (internal/snapshot), so checking out an old version
// can never revert work outside the artifact directory. SQLite holds only the
// metadata needed to list and navigate artifacts.
package design

import "time"

// Kind enumerates the artifact kinds supported in v1. Further kinds (mobile,
// document, dashboard, diagram) are deliberately deferred.
type Kind string

const (
	// KindWeb is a web prototype: one or more pages rendered at a viewport.
	KindWeb Kind = "web"
	// KindDeck is a slide deck: a single document whose slides are addressed by
	// index and exported one per PDF page.
	KindDeck Kind = "deck"
)

// ValidKind reports whether k is a kind this version supports.
func ValidKind(k Kind) bool {
	switch k {
	case KindWeb, KindDeck:
		return true
	default:
		return false
	}
}

// Artifact is the metadata record of a design artifact. The files themselves
// live in Dir, relative to the project working directory.
type Artifact struct {
	ID             string    `json:"id"` // dsg_<hex>
	SessionID      string    `json:"session_id,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
	Title          string    `json:"title"`
	Slug           string    `json:"slug"`
	Dir            string    `json:"dir"` // project-relative, slash-separated
	Kind           Kind      `json:"kind"`
	SkillID        string    `json:"skill_id,omitempty"`
	DesignSystemID string    `json:"design_system_id,omitempty"`
	CurrentVersion int       `json:"current_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Version is one accepted iteration of an artifact, backed by a scoped
// snapshot of its directory.
type Version struct {
	ArtifactID string    `json:"artifact_id"`
	Number     int       `json:"number"`
	SnapshotID string    `json:"snapshot_id"`
	Summary    string    `json:"summary"`
	Critique   *Critique `json:"critique,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Rect is a layout box in CSS pixels, as reported by the renderer.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Node is one entry of the structure index the inspector builds after a
// render. NodeID is the stable data-pando-id attribute injected at render time
// and is what a UI selection resolves to (design://<node_id>).
type Node struct {
	ArtifactID string            `json:"artifact_id"`
	Version    int               `json:"version"`
	NodeID     string            `json:"node_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Selector   string            `json:"selector,omitempty"`
	Role       string            `json:"role,omitempty"`
	Text       string            `json:"text,omitempty"`
	Slide      int               `json:"slide,omitempty"` // deck only
	Box        Rect              `json:"box"`
	Styles     map[string]string `json:"styles,omitempty"`
}

// Issue severities produced by the critic and the accessibility pass.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityError    = "error"
	SeverityBlocking = "blocking"
)

// Issue is a single actionable finding against a version. NodeID (and Slide for
// decks) let the UI turn an issue into a selection.
type Issue struct {
	// Code is the stable rule identifier for a finding the deterministic audit
	// produced ("a11y.contrast"), empty for a finding a critic wrote in prose.
	// It is what lets a UI group findings and a caller suppress a rule without
	// matching on its message.
	Code     string `json:"code,omitempty"`
	Severity string `json:"severity"`
	NodeID   string `json:"node_id,omitempty"`
	Slide    int    `json:"slide,omitempty"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// Critique is one critic pass over a version.
type Critique struct {
	ID         string    `json:"id"`
	ArtifactID string    `json:"artifact_id"`
	Version    int       `json:"version"`
	Score      float64   `json:"score"` // 0-10
	Summary    string    `json:"summary,omitempty"`
	Issues     []Issue   `json:"issues"`
	CreatedAt  time.Time `json:"created_at"`
}
