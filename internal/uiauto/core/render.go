package core

import (
	"fmt"
	"sort"
	"strings"
)

// RenderOptions controls the agent-facing compact renderer.
type RenderOptions struct {
	// MaxNodes caps how many element lines are emitted; 0 means unbounded.
	MaxNodes int
	// MaxDepth caps how many levels below the root are descended; 0 means
	// unbounded.
	MaxDepth int
	// IncludeBounds appends bounds="x,y,w,h" to each line.
	IncludeBounds bool
	// IncludeInvisible includes elements with Visible == false. When
	// false (the default), invisible subtrees are skipped entirely.
	IncludeInvisible bool
}

// isSemanticallyEmpty reports whether el carries no information worth
// showing on its own line: no name, no value, no description and no
// actions. Such containers are collapsed into their children by the
// renderer instead of emitting a bare "group" line.
func isSemanticallyEmpty(el *Element) bool {
	return el.Name == "" && el.Value == "" && el.Description == "" && len(el.Actions) == 0
}

// renderState carries the mutable bookkeeping for a single RenderTree call.
type renderState struct {
	snap      *Snapshot
	opts      RenderOptions
	lines     []string
	emitted   int
	truncated bool
	remaining int // nodes skipped after the budget was hit
}

// RenderTree renders snap as a compact, indented, agent-facing tree
// starting from a synthetic header line describing the window/app, then
// one line per element in the form:
//
//	@<snapshotID>:<elemID> role "name" value="..." [flags...]
//
// Semantically empty container nodes (no name/value/description/actions)
// are collapsed: they are not printed, but their children are still
// rendered at the collapsed node's depth.
func RenderTree(snap *Snapshot, opts RenderOptions) string {
	if snap == nil || snap.Root == nil {
		return ""
	}
	st := &renderState{snap: snap, opts: opts}

	header := "WINDOW"
	if snap.Root.Role != "" && snap.Root.Role != RoleWindow {
		header = strings.ToUpper(string(snap.Root.Role))
	}
	title := snap.Root.Name
	if title == "" {
		title = snap.WindowID
	}
	headerLine := header
	if title != "" {
		headerLine += fmt.Sprintf(" %q", title)
	}
	if snap.AppID != "" {
		headerLine += fmt.Sprintf(" (app: %s)", snap.AppID)
	}
	st.lines = append(st.lines, headerLine)

	st.renderChildren(snap.Root, 0)

	if st.truncated {
		st.lines = append(st.lines, fmt.Sprintf("... %d more nodes not shown; narrow with desktop_find", st.remaining))
	}
	return strings.Join(st.lines, "\n")
}

// renderChildren renders el's children (recursing through semantically
// empty nodes without consuming a depth level's visual indent budget for
// them) at the given depth.
func (st *renderState) renderChildren(el *Element, depth int) {
	children := st.resolveChildren(el)
	for _, child := range children {
		st.renderNode(child, depth)
	}
}

// resolveChildren looks up an element's children within the snapshot,
// applying visibility filtering, in stable (traversal) order.
func (st *renderState) resolveChildren(el *Element) []*Element {
	ids := el.ChildIDs
	children := make([]*Element, 0, len(ids))
	for _, ref := range ids {
		_, id, err := ParseElementRef(string(ref))
		if err != nil {
			continue
		}
		child, ok := st.snap.Elements[id]
		if !ok || child == nil {
			continue
		}
		if !st.opts.IncludeInvisible && !child.Visible {
			continue
		}
		children = append(children, child)
	}
	return children
}

// renderNode renders a single node (or, if it is semantically empty,
// collapses it and renders its children in its place) at depth.
func (st *renderState) renderNode(el *Element, depth int) {
	if st.opts.MaxDepth > 0 && depth > st.opts.MaxDepth {
		st.countSkipped(el)
		return
	}
	if isSemanticallyEmpty(el) {
		st.renderChildren(el, depth)
		return
	}
	if st.opts.MaxNodes > 0 && st.emitted >= st.opts.MaxNodes {
		st.countSkipped(el)
		return
	}

	indent := strings.Repeat("  ", depth)
	st.lines = append(st.lines, indent+formatElementLine(el, st.opts))
	st.emitted++

	st.renderChildren(el, depth+1)
}

// countSkipped marks the render as truncated and accounts el plus its
// entire subtree as "not shown".
func (st *renderState) countSkipped(el *Element) {
	st.truncated = true
	st.remaining += 1 + countDescendants(st.snap, el)
}

func countDescendants(snap *Snapshot, el *Element) int {
	total := 0
	for _, ref := range el.ChildIDs {
		_, id, err := ParseElementRef(string(ref))
		if err != nil {
			continue
		}
		child, ok := snap.Elements[id]
		if !ok || child == nil {
			continue
		}
		total += 1 + countDescendants(snap, child)
	}
	return total
}

// formatElementLine renders the single-line representation of el, without
// leading indentation.
func formatElementLine(el *Element, opts RenderOptions) string {
	var sb strings.Builder
	sb.WriteString(string(el.ID))
	sb.WriteString(" ")
	sb.WriteString(string(el.Role))
	if el.Name != "" {
		sb.WriteString(fmt.Sprintf(" %q", el.Name))
	}
	if el.Value != "" {
		sb.WriteString(fmt.Sprintf(" value=%q", el.Value))
	}
	if el.Description != "" {
		sb.WriteString(fmt.Sprintf(" description=%q", el.Description))
	}
	if opts.IncludeBounds && !el.Bounds.Empty() {
		sb.WriteString(fmt.Sprintf(" bounds=%d,%d,%d,%d", el.Bounds.X, el.Bounds.Y, el.Bounds.W, el.Bounds.H))
	}
	var flags []string
	if !el.Enabled {
		flags = append(flags, "disabled")
	}
	if el.Focused {
		flags = append(flags, "focused")
	}
	if roleHasCheckedFlag(el.Role) && el.Value == "true" {
		flags = append(flags, "checked")
	}
	sort.Strings(flags)
	for _, f := range flags {
		sb.WriteString(" ")
		sb.WriteString(f)
	}
	return sb.String()
}

func roleHasCheckedFlag(r Role) bool {
	return r == RoleCheckbox || r == RoleRadio
}

// RenderElements renders a flat list of elements (e.g. a desktop_find
// result), one line per element, honouring MaxNodes/IncludeBounds and
// IncludeInvisible from opts. MaxDepth is not applicable to a flat list and
// is ignored.
func RenderElements(elements []*Element, opts RenderOptions) string {
	var lines []string
	emitted := 0
	skipped := 0
	for _, el := range elements {
		if el == nil {
			continue
		}
		if !opts.IncludeInvisible && !el.Visible {
			continue
		}
		if opts.MaxNodes > 0 && emitted >= opts.MaxNodes {
			skipped++
			continue
		}
		lines = append(lines, formatElementLine(el, opts))
		emitted++
	}
	if skipped > 0 {
		lines = append(lines, fmt.Sprintf("... %d more nodes not shown; narrow with desktop_find", skipped))
	}
	return strings.Join(lines, "\n")
}
