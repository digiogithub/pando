package design

import (
	"fmt"
	"sort"
	"strings"
)

// defaultInspectLimit is how many nodes one inspect page returns. The index is
// read by a model, so the default is a page, not the whole document.
const defaultInspectLimit = 40

// maxInspectLimit caps what a caller can ask for in one page.
const maxInspectLimit = 200

// InspectOptions narrows and pages the structure index.
type InspectOptions struct {
	// NodeID restricts the result to one node and its descendants.
	NodeID string
	// Selector matches nodes whose selector or role contains this string.
	Selector string
	// Text matches nodes whose text contains this string (case-insensitive).
	Text string
	// Slide restricts the result to one deck slide; -1 for every slide.
	Slide int
	// Depth limits how far below the root nodes the result descends.
	// Zero means unlimited.
	Depth int
	// Offset and Limit page the result.
	Offset int
	Limit  int
	// IncludeStyles carries the computed-style subset. Off by default: styles
	// are the largest part of a node and are rarely needed for every node.
	IncludeStyles bool
	// StyleProps narrows which style properties survive when IncludeStyles is
	// set. Empty keeps all indexed properties.
	StyleProps []string
	// MaxTextLen truncates node text. Zero keeps the indexed text.
	MaxTextLen int
}

// InspectResult is one page of the structure index.
type InspectResult struct {
	ArtifactID string `json:"artifact_id"`
	Version    int    `json:"version"`
	// Total is how many nodes matched before paging.
	Total  int    `json:"total"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
	Nodes  []Node `json:"nodes"`
	// NextOffset is the offset of the next page, or -1 when this is the last.
	NextOffset int `json:"next_offset"`
}

// Inspect filters, trims and pages a node index. It works on any node slice, so
// the same code serves a fresh render and the stored index of an old version.
func Inspect(nodes []Node, opts InspectOptions) InspectResult {
	if opts.Limit <= 0 {
		opts.Limit = defaultInspectLimit
	}
	if opts.Limit > maxInspectLimit {
		opts.Limit = maxInspectLimit
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	matched := filterNodes(nodes, opts)
	result := InspectResult{
		Total:      len(matched),
		Offset:     opts.Offset,
		Limit:      opts.Limit,
		NextOffset: -1,
		Nodes:      []Node{},
	}
	if len(matched) > 0 {
		result.ArtifactID = matched[0].ArtifactID
		result.Version = matched[0].Version
	}
	if opts.Offset >= len(matched) {
		return result
	}

	end := opts.Offset + opts.Limit
	if end > len(matched) {
		end = len(matched)
	} else if end < len(matched) {
		result.NextOffset = end
	}

	page := make([]Node, 0, end-opts.Offset)
	for _, n := range matched[opts.Offset:end] {
		page = append(page, trimNode(n, opts))
	}
	result.Nodes = page
	return result
}

// filterNodes applies every narrowing option, in document order.
func filterNodes(nodes []Node, opts InspectOptions) []Node {
	byID := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		byID[n.NodeID] = n
	}

	// Subtree restriction: keep the node and everything under it.
	inSubtree := func(n Node) bool {
		if opts.NodeID == "" {
			return true
		}
		current := n
		for i := 0; i < defaultMaxDepth+2; i++ {
			if current.NodeID == opts.NodeID {
				return true
			}
			parent, ok := byID[current.ParentID]
			if !ok {
				return false
			}
			current = parent
		}
		return false
	}

	depthOf := func(n Node) int {
		depth := 0
		current := n
		for i := 0; i < defaultMaxDepth+2; i++ {
			parent, ok := byID[current.ParentID]
			if !ok {
				return depth
			}
			if opts.NodeID != "" && current.NodeID == opts.NodeID {
				return depth
			}
			depth++
			current = parent
		}
		return depth
	}

	selectorNeedle := strings.ToLower(opts.Selector)
	textNeedle := strings.ToLower(opts.Text)

	matched := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if opts.Slide >= 0 && n.Slide != opts.Slide {
			continue
		}
		if !inSubtree(n) {
			continue
		}
		if opts.Depth > 0 && depthOf(n) > opts.Depth {
			continue
		}
		if selectorNeedle != "" &&
			!strings.Contains(strings.ToLower(n.Selector), selectorNeedle) &&
			!strings.Contains(strings.ToLower(n.Role), selectorNeedle) {
			continue
		}
		if textNeedle != "" && !strings.Contains(strings.ToLower(n.Text), textNeedle) {
			continue
		}
		matched = append(matched, n)
	}

	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Slide != matched[j].Slide {
			return matched[i].Slide < matched[j].Slide
		}
		if matched[i].Box.Y != matched[j].Box.Y {
			return matched[i].Box.Y < matched[j].Box.Y
		}
		return matched[i].Box.X < matched[j].Box.X
	})
	return matched
}

// trimNode drops what the caller did not ask for, which is where most of the
// token budget is won.
func trimNode(n Node, opts InspectOptions) Node {
	if !opts.IncludeStyles {
		n.Styles = nil
	} else if len(opts.StyleProps) > 0 && len(n.Styles) > 0 {
		trimmed := make(map[string]string, len(opts.StyleProps))
		for _, prop := range opts.StyleProps {
			if value, ok := n.Styles[prop]; ok {
				trimmed[prop] = value
			}
		}
		n.Styles = trimmed
	}
	if opts.MaxTextLen > 0 && len(n.Text) > opts.MaxTextLen {
		n.Text = n.Text[:opts.MaxTextLen] + "…"
	}
	return n
}

// Text renders an inspect page as compact lines for a tool result. One node per
// line keeps it readable for a model without spending a JSON envelope per node.
func (r InspectResult) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "nodes %d-%d of %d\n", r.Offset+1, r.Offset+len(r.Nodes), r.Total)
	if len(r.Nodes) == 0 {
		b.WriteString("(no matching nodes)\n")
		return b.String()
	}
	for _, n := range r.Nodes {
		fmt.Fprintf(&b, "%s  %s  [%.0fx%.0f @ %.0f,%.0f]", n.NodeID, n.Selector, n.Box.W, n.Box.H, n.Box.X, n.Box.Y)
		if n.Slide > 0 {
			fmt.Fprintf(&b, " slide=%d", n.Slide)
		}
		if n.Text != "" {
			fmt.Fprintf(&b, " %q", n.Text)
		}
		if len(n.Styles) > 0 {
			props := make([]string, 0, len(n.Styles))
			for prop := range n.Styles {
				props = append(props, prop)
			}
			sort.Strings(props)
			parts := make([]string, 0, len(props))
			for _, prop := range props {
				parts = append(parts, prop+"="+n.Styles[prop])
			}
			fmt.Fprintf(&b, "  {%s}", strings.Join(parts, "; "))
		}
		b.WriteByte('\n')
	}
	if r.NextOffset >= 0 {
		fmt.Fprintf(&b, "more nodes available: offset=%d\n", r.NextOffset)
	}
	return b.String()
}
