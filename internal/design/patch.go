package design

import (
	"fmt"
	"sort"
	"strings"
)

// Patch operations. Each one targets a single element resolved by selector (or,
// through the node index, by the data-pando-id a render stamped on it).
const (
	OpSetText     = "set_text"      // replace the element's children with escaped text
	OpSetHTML     = "set_html"      // replace the element's children with raw markup
	OpSetAttr     = "set_attr"      // add or update one attribute
	OpRemoveAttr  = "remove_attr"   // drop one attribute
	OpSetStyle    = "set_style"     // merge declarations into the inline style attribute
	OpAddClass    = "add_class"     // append a class if missing
	OpRemoveClass = "remove_class"  // drop a class if present
	OpInsertHTML  = "insert_html"   // insert markup relative to the element
	OpReplaceHTML = "replace_outer" // replace the whole element
	OpRemove      = "remove"        // delete the whole element
)

// Insert positions accepted by OpInsertHTML.
const (
	PositionBefore  = "before"
	PositionAfter   = "after"
	PositionPrepend = "prepend"
	PositionAppend  = "append"
)

// PatchOp is one edit request. Exactly one of NodeID or Selector must be set;
// File defaults to the artifact's entry document.
type PatchOp struct {
	NodeID   string            `json:"node_id,omitempty"`
	Selector string            `json:"selector,omitempty"`
	File     string            `json:"file,omitempty"`
	Op       string            `json:"op"`
	Attr     string            `json:"attr,omitempty"`
	Value    string            `json:"value,omitempty"`
	Style    map[string]string `json:"style,omitempty"`
	Class    string            `json:"class,omitempty"`
	HTML     string            `json:"html,omitempty"`
	Position string            `json:"position,omitempty"`
	// All allows an operation to apply to every match instead of failing when a
	// selector is ambiguous.
	All bool `json:"all,omitempty"`
}

// PatchChange reports what one operation did, for the tool response and the
// permission prompt.
type PatchChange struct {
	Op       string `json:"op"`
	Selector string `json:"selector"`
	Matches  int    `json:"matches"`
	Detail   string `json:"detail,omitempty"`
}

// edit is a resolved splice into the source bytes.
type edit struct {
	start, end int
	replace    string
}

// ApplyPatch applies ops to the HTML source and returns the new bytes together
// with a description of every change. The source is spliced, never
// reserialised: bytes outside the targeted ranges are preserved exactly.
func ApplyPatch(src []byte, ops []PatchOp) ([]byte, []PatchChange, error) {
	if len(ops) == 0 {
		return src, nil, nil
	}
	doc := parseHTMLDoc(src)

	var edits []edit
	changes := make([]PatchChange, 0, len(ops))

	for i, op := range ops {
		sel, err := parseSelector(op.Selector)
		if err != nil {
			return nil, nil, fmt.Errorf("op %d: %w", i+1, err)
		}
		matches := doc.Match(sel)
		if len(matches) == 0 {
			return nil, nil, fmt.Errorf("op %d: selector %q matched no element in the source; the renderer indexes the live DOM, so a node created by a script cannot be patched — edit the source that produces it", i+1, op.Selector)
		}
		if len(matches) > 1 && !op.All {
			return nil, nil, fmt.Errorf("op %d: selector %q matched %d elements; narrow it or set \"all\": true", i+1, op.Selector, len(matches))
		}

		detail := ""
		for _, el := range matches {
			e, d, err := buildEdit(src, el, op)
			if err != nil {
				return nil, nil, fmt.Errorf("op %d: %w", i+1, err)
			}
			detail = d
			edits = append(edits, e...)
		}
		changes = append(changes, PatchChange{
			Op:       op.Op,
			Selector: op.Selector,
			Matches:  len(matches),
			Detail:   detail,
		})
	}

	out, err := applyEdits(src, edits)
	if err != nil {
		return nil, nil, err
	}
	return out, changes, nil
}

// buildEdit turns one operation on one element into splices.
func buildEdit(src []byte, el *htmlElem, op PatchOp) ([]edit, string, error) {
	switch op.Op {
	case OpSetText:
		if el.void {
			return nil, "", fmt.Errorf("<%s> is a void element and has no text content", el.tag)
		}
		return []edit{{el.innerStart, el.innerEnd, escapeText(op.Value)}},
			fmt.Sprintf("text of <%s> set to %q", el.tag, truncateDetail(op.Value)), nil

	case OpSetHTML:
		if el.void {
			return nil, "", fmt.Errorf("<%s> is a void element and has no content", el.tag)
		}
		return []edit{{el.innerStart, el.innerEnd, op.HTML}},
			fmt.Sprintf("content of <%s> replaced", el.tag), nil

	case OpSetAttr:
		if op.Attr == "" {
			return nil, "", fmt.Errorf("set_attr needs \"attr\"")
		}
		e, err := setAttrEdit(el, op.Attr, op.Value)
		if err != nil {
			return nil, "", err
		}
		return e, fmt.Sprintf("<%s> %s=%q", el.tag, op.Attr, truncateDetail(op.Value)), nil

	case OpRemoveAttr:
		if op.Attr == "" {
			return nil, "", fmt.Errorf("remove_attr needs \"attr\"")
		}
		a, ok := el.attr(op.Attr)
		if !ok {
			return nil, fmt.Sprintf("<%s> had no %s attribute", el.tag, op.Attr), nil
		}
		start := a.start
		// Swallow one run of preceding whitespace so the tag stays tidy.
		for start > el.startTagStart && isHTMLSpace(src[start-1]) {
			start--
		}
		return []edit{{start, a.end, ""}}, fmt.Sprintf("<%s> %s removed", el.tag, op.Attr), nil

	case OpSetStyle:
		if len(op.Style) == 0 {
			return nil, "", fmt.Errorf("set_style needs \"style\"")
		}
		merged := mergeStyle(el.attrValue("style"), op.Style)
		e, err := setAttrEdit(el, "style", merged)
		if err != nil {
			return nil, "", err
		}
		return e, fmt.Sprintf("<%s> style: %s", el.tag, truncateDetail(merged)), nil

	case OpAddClass:
		if op.Class == "" {
			return nil, "", fmt.Errorf("add_class needs \"class\"")
		}
		if el.hasClass(op.Class) {
			return nil, fmt.Sprintf("<%s> already had class %s", el.tag, op.Class), nil
		}
		classes := append(el.classes(), op.Class)
		e, err := setAttrEdit(el, "class", strings.Join(classes, " "))
		if err != nil {
			return nil, "", err
		}
		return e, fmt.Sprintf("<%s> class +%s", el.tag, op.Class), nil

	case OpRemoveClass:
		if op.Class == "" {
			return nil, "", fmt.Errorf("remove_class needs \"class\"")
		}
		var kept []string
		for _, c := range el.classes() {
			if c != op.Class {
				kept = append(kept, c)
			}
		}
		if len(kept) == len(el.classes()) {
			return nil, fmt.Sprintf("<%s> had no class %s", el.tag, op.Class), nil
		}
		e, err := setAttrEdit(el, "class", strings.Join(kept, " "))
		if err != nil {
			return nil, "", err
		}
		return e, fmt.Sprintf("<%s> class -%s", el.tag, op.Class), nil

	case OpInsertHTML:
		if op.HTML == "" {
			return nil, "", fmt.Errorf("insert_html needs \"html\"")
		}
		switch op.Position {
		case PositionBefore:
			return []edit{{el.outerStart, el.outerStart, op.HTML}},
				fmt.Sprintf("markup inserted before <%s>", el.tag), nil
		case "", PositionAfter:
			return []edit{{el.outerEnd, el.outerEnd, op.HTML}},
				fmt.Sprintf("markup inserted after <%s>", el.tag), nil
		case PositionPrepend:
			if el.void {
				return nil, "", fmt.Errorf("<%s> is a void element and cannot hold children", el.tag)
			}
			return []edit{{el.innerStart, el.innerStart, op.HTML}},
				fmt.Sprintf("markup prepended to <%s>", el.tag), nil
		case PositionAppend:
			if el.void {
				return nil, "", fmt.Errorf("<%s> is a void element and cannot hold children", el.tag)
			}
			return []edit{{el.innerEnd, el.innerEnd, op.HTML}},
				fmt.Sprintf("markup appended to <%s>", el.tag), nil
		default:
			return nil, "", fmt.Errorf("unknown position %q (before, after, prepend, append)", op.Position)
		}

	case OpReplaceHTML:
		if op.HTML == "" {
			return nil, "", fmt.Errorf("replace_outer needs \"html\"")
		}
		return []edit{{el.outerStart, el.outerEnd, op.HTML}},
			fmt.Sprintf("<%s> replaced", el.tag), nil

	case OpRemove:
		return []edit{{el.outerStart, el.outerEnd, ""}},
			fmt.Sprintf("<%s> removed", el.tag), nil

	default:
		return nil, "", fmt.Errorf("unknown op %q", op.Op)
	}
}

// setAttrEdit rewrites just the value of an existing attribute, or splices a
// new attribute into the start tag. An empty value on class/style drops the
// attribute rather than leaving an empty one behind.
func setAttrEdit(el *htmlElem, name, value string) ([]edit, error) {
	name = strings.ToLower(name)
	a, ok := el.attr(name)
	if !ok {
		if value == "" && (name == "class" || name == "style") {
			return nil, nil
		}
		at, err := el.insertPoint()
		if err != nil {
			return nil, err
		}
		return []edit{{at, at, fmt.Sprintf(` %s="%s"`, name, escapeAttr(value))}}, nil
	}
	if value == "" && (name == "class" || name == "style") {
		return []edit{{a.start, a.end, ""}}, nil
	}
	if a.valStart < 0 {
		// Bare attribute (e.g. `hidden`): give it a value.
		return []edit{{a.start, a.end, fmt.Sprintf(`%s="%s"`, name, escapeAttr(value))}}, nil
	}
	escaped := escapeAttr(value)
	if a.quote == '\'' {
		escaped = strings.ReplaceAll(escaped, "&quot;", `"`)
		escaped = strings.ReplaceAll(escaped, "'", "&#39;")
	}
	return []edit{{a.valStart, a.valEnd, escaped}}, nil
}

// mergeStyle merges declarations into an inline style attribute. Existing
// declarations keep their position and order; new ones are appended sorted by
// property so that the same patch always produces the same bytes.
func mergeStyle(current string, updates map[string]string) string {
	type decl struct{ prop, val string }
	var order []decl
	seen := make(map[string]int, len(updates))

	for _, raw := range strings.Split(current, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		colon := strings.Index(raw, ":")
		if colon < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(raw[:colon]))
		val := strings.TrimSpace(raw[colon+1:])
		seen[prop] = len(order)
		order = append(order, decl{prop, val})
	}

	added := make([]string, 0, len(updates))
	for prop := range updates {
		added = append(added, strings.ToLower(strings.TrimSpace(prop)))
	}
	sort.Strings(added)

	for _, prop := range added {
		val := strings.TrimSpace(updates[prop])
		if val == "" {
			val = strings.TrimSpace(updates[strings.ToLower(prop)])
		}
		if idx, ok := seen[prop]; ok {
			if val == "" {
				order[idx].prop = "" // marked for removal
				continue
			}
			order[idx].val = val
			continue
		}
		if val == "" {
			continue
		}
		seen[prop] = len(order)
		order = append(order, decl{prop, val})
	}

	parts := make([]string, 0, len(order))
	for _, d := range order {
		if d.prop == "" {
			continue
		}
		parts = append(parts, d.prop+": "+d.val)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// applyEdits splices the edits into src, rejecting overlapping ranges so a
// patch never produces silently mangled output.
func applyEdits(src []byte, edits []edit) ([]byte, error) {
	if len(edits) == 0 {
		return src, nil
	}
	sorted := make([]edit, len(edits))
	copy(sorted, edits)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].start != sorted[j].start {
			return sorted[i].start < sorted[j].start
		}
		return sorted[i].end < sorted[j].end
	})
	for i := 1; i < len(sorted); i++ {
		prev, cur := sorted[i-1], sorted[i]
		// Pure insertions at the same offset are allowed; overlapping
		// replacements are not.
		if cur.start < prev.end {
			return nil, fmt.Errorf("design: conflicting patch operations touch the same region (bytes %d-%d and %d-%d); apply them in separate calls", prev.start, prev.end, cur.start, cur.end)
		}
	}

	var out strings.Builder
	out.Grow(len(src))
	cursor := 0
	for _, e := range sorted {
		if e.start < cursor || e.end > len(src) || e.start > e.end {
			return nil, fmt.Errorf("design: invalid patch range %d-%d", e.start, e.end)
		}
		out.Write(src[cursor:e.start])
		out.WriteString(e.replace)
		cursor = e.end
	}
	out.Write(src[cursor:])
	return []byte(out.String()), nil
}

func truncateDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 60 {
		return s
	}
	return s[:57] + "..."
}
