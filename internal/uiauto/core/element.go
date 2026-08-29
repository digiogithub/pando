// Package core provides the platform-independent building blocks of the
// Pando Desktop Controller: a normalized accessibility-tree element model,
// a selector DSL, snapshot/ref bookkeeping, action resolution and rendering
// for LLM consumption. No package in this directory performs any OS call;
// platform backends live under internal/uiauto/platform.
package core

import (
	"fmt"
	"strings"
)

// ElementRef is a qualified reference to an Element, always scoped to the
// snapshot that produced it, of the form "@<snapshotID>:<elemID>".
type ElementRef string

// ParseElementRef splits a qualified ElementRef into its snapshot id and
// element id components. It returns an INVALID_ARGS DesktopError if ref is
// not well formed.
func ParseElementRef(ref string) (snapshotID string, elemID string, err error) {
	if !strings.HasPrefix(ref, "@") {
		return "", "", NewInvalidArgsError(fmt.Sprintf("element ref %q must start with '@'", ref))
	}
	body := strings.TrimPrefix(ref, "@")
	idx := strings.IndexByte(body, ':')
	if idx <= 0 || idx == len(body)-1 {
		return "", "", NewInvalidArgsError(fmt.Sprintf("element ref %q must be of the form @<snapshotID>:<elemID>", ref))
	}
	snapshotID = body[:idx]
	elemID = body[idx+1:]
	if snapshotID == "" || elemID == "" {
		return "", "", NewInvalidArgsError(fmt.Sprintf("element ref %q has an empty snapshot or element id", ref))
	}
	return snapshotID, elemID, nil
}

// FormatElementRef builds a qualified ElementRef from a snapshot id and an
// element id.
func FormatElementRef(snapshotID, elemID string) ElementRef {
	return ElementRef("@" + snapshotID + ":" + elemID)
}

// Bounds is an axis-aligned rectangle in screen coordinates.
type Bounds struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Center returns the midpoint of the bounds, suitable as a physical click
// target.
func (b Bounds) Center() (x, y int) {
	return b.X + b.W/2, b.Y + b.H/2
}

// Empty reports whether the bounds carry no usable size (e.g. an element
// that was never laid out or that the backend could not measure).
func (b Bounds) Empty() bool {
	return b.W <= 0 || b.H <= 0
}

// NativeData is the per-backend escape hatch: it preserves the raw platform
// role/subrole and any extra attributes a normalized Element cannot express.
type NativeData struct {
	// Platform identifies the backend that produced this data, e.g.
	// "atspi", "uia", "ax", "cdp".
	Platform string `json:"platform,omitempty"`
	// Role is the raw, un-normalized platform role (e.g. AT-SPI "push button").
	Role string `json:"role,omitempty"`
	// SubRole is a platform-specific refinement (e.g. macOS AXSubrole).
	SubRole string `json:"subRole,omitempty"`
	// Data carries any additional backend-specific attributes.
	Data map[string]any `json:"data,omitempty"`
}

// Element is the normalized, platform-independent representation of a node
// in an accessibility tree.
type Element struct {
	ID          ElementRef   `json:"id"`
	Role        Role         `json:"role"`
	Name        string       `json:"name,omitempty"`
	Value       string       `json:"value,omitempty"`
	Description string       `json:"description,omitempty"`
	Bounds      Bounds       `json:"bounds,omitempty"`
	Enabled     bool         `json:"enabled"`
	Visible     bool         `json:"visible"`
	Focused     bool         `json:"focused"`
	ParentID    ElementRef   `json:"parentId,omitempty"`
	ChildIDs    []ElementRef `json:"childIds,omitempty"`
	Actions     []ActionKind `json:"actions,omitempty"`

	// Backend is the name of the backend that produced this element
	// ("atspi", "uia", "ax", "cdp", "null").
	Backend string `json:"backend,omitempty"`
	// AppID identifies the owning application, backend-specific.
	AppID string `json:"appId,omitempty"`
	// WindowID identifies the owning window, backend-specific.
	WindowID string `json:"windowId,omitempty"`

	Native NativeData `json:"native,omitempty"`
}
