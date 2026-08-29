package core

import (
	"strings"
	"testing"
)

// buildRenderSnapshot builds:
//
//	root (window "Settings", app TestApp)
//	  group (empty, collapsed)
//	    heading "Title"
//	    button "Save"
//	  checkbox "Enable memory" (value=true)
func buildRenderSnapshot(id string) *Snapshot {
	ref := func(e string) ElementRef { return FormatElementRef(id, e) }

	root := &Element{ID: ref("e1"), Role: RoleWindow, Name: "Settings", Visible: true, Enabled: true}
	group := &Element{ID: ref("e2"), Role: RoleGroup, ParentID: root.ID, Visible: true, Enabled: true} // semantically empty
	heading := &Element{ID: ref("e3"), Role: RoleHeading, Name: "Title", ParentID: group.ID, Visible: true, Enabled: true}
	button := &Element{ID: ref("e4"), Role: RoleButton, Name: "Save", ParentID: group.ID, Visible: true, Enabled: true, Actions: []ActionKind{ActionInvoke}}
	checkbox := &Element{ID: ref("e5"), Role: RoleCheckbox, Name: "Enable memory", Value: "true", ParentID: root.ID, Visible: true, Enabled: true}

	group.ChildIDs = []ElementRef{heading.ID, button.ID}
	root.ChildIDs = []ElementRef{group.ID, checkbox.ID}

	return &Snapshot{
		ID:      id,
		Backend: "null",
		AppID:   "TestApp",
		Root:    root,
		Elements: map[string]*Element{
			"e1": root, "e2": group, "e3": heading, "e4": button, "e5": checkbox,
		},
	}
}

func TestRenderTree_Header(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	out := RenderTree(snap, RenderOptions{})
	if !strings.HasPrefix(out, `WINDOW "Settings" (app: TestApp)`) {
		t.Fatalf("unexpected header line: %q", out)
	}
}

func TestRenderTree_CollapsesEmptyGroup(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	out := RenderTree(snap, RenderOptions{})
	if strings.Contains(out, "group") {
		t.Fatalf("expected semantically empty group to be collapsed, got:\n%s", out)
	}
	if !strings.Contains(out, `heading "Title"`) {
		t.Fatalf("expected heading to still be rendered (as a child of the collapsed group), got:\n%s", out)
	}
	if !strings.Contains(out, `button "Save"`) {
		t.Fatalf("expected button to still be rendered, got:\n%s", out)
	}
}

func TestRenderTree_ElementRefsAndFlags(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	out := RenderTree(snap, RenderOptions{})
	if !strings.Contains(out, "@s1:e4 button \"Save\"") {
		t.Fatalf("expected qualified ref line for button, got:\n%s", out)
	}
	if !strings.Contains(out, "@s1:e5 checkbox \"Enable memory\" value=\"true\" checked") {
		t.Fatalf("expected checked flag on checkbox line, got:\n%s", out)
	}
}

func TestRenderTree_MaxNodesTruncation(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	out := RenderTree(snap, RenderOptions{MaxNodes: 2})
	if !strings.Contains(out, "more nodes not shown; narrow with desktop_find") {
		t.Fatalf("expected truncation notice, got:\n%s", out)
	}
	// Header + exactly 2 element lines + truncation notice.
	lines := strings.Split(out, "\n")
	elementLines := 0
	for _, l := range lines {
		if strings.Contains(l, "@s1:") {
			elementLines++
		}
	}
	if elementLines != 2 {
		t.Fatalf("expected exactly 2 element lines under MaxNodes=2, got %d:\n%s", elementLines, out)
	}
}

func TestRenderTree_MaxDepth(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	// Depth 0 below root: group is depth 0 but collapsed, its children
	// (heading/button) sit at depth 0 too since the group doesn't consume
	// a level. checkbox also sits at depth 0. So MaxDepth=0 should still
	// show all of them; verify nothing panics and header renders.
	out := RenderTree(snap, RenderOptions{MaxDepth: 0})
	if !strings.Contains(out, "Settings") {
		t.Fatalf("expected header to render regardless of MaxDepth, got:\n%s", out)
	}
}

func TestRenderTree_IncludeBounds(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	snap.Elements["e4"].Bounds = Bounds{X: 1, Y: 2, W: 3, H: 4}
	out := RenderTree(snap, RenderOptions{IncludeBounds: true})
	if !strings.Contains(out, "bounds=1,2,3,4") {
		t.Fatalf("expected bounds to be rendered, got:\n%s", out)
	}
}

func TestRenderTree_ExcludesInvisibleByDefault(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	snap.Elements["e4"].Visible = false
	out := RenderTree(snap, RenderOptions{})
	if strings.Contains(out, `"Save"`) {
		t.Fatalf("expected invisible button to be excluded by default, got:\n%s", out)
	}
}

func TestRenderTree_IncludeInvisible(t *testing.T) {
	snap := buildRenderSnapshot("s1")
	snap.Elements["e4"].Visible = false
	out := RenderTree(snap, RenderOptions{IncludeInvisible: true})
	if !strings.Contains(out, `"Save"`) {
		t.Fatalf("expected invisible button to be included with IncludeInvisible, got:\n%s", out)
	}
}

func TestRenderTree_EmptySnapshot(t *testing.T) {
	if out := RenderTree(nil, RenderOptions{}); out != "" {
		t.Fatalf("expected empty string for nil snapshot, got %q", out)
	}
	if out := RenderTree(&Snapshot{}, RenderOptions{}); out != "" {
		t.Fatalf("expected empty string for a snapshot with no root, got %q", out)
	}
}

func TestRenderElements_Basic(t *testing.T) {
	els := []*Element{
		{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Name: "A", Visible: true, Enabled: true},
		{ID: FormatElementRef("s1", "e2"), Role: RoleButton, Name: "B", Visible: true, Enabled: true},
	}
	out := RenderElements(els, RenderOptions{})
	if !strings.Contains(out, `@s1:e1 button "A"`) || !strings.Contains(out, `@s1:e2 button "B"`) {
		t.Fatalf("unexpected flat render output:\n%s", out)
	}
}

func TestRenderElements_Truncation(t *testing.T) {
	els := []*Element{
		{ID: FormatElementRef("s1", "e1"), Role: RoleButton, Name: "A", Visible: true, Enabled: true},
		{ID: FormatElementRef("s1", "e2"), Role: RoleButton, Name: "B", Visible: true, Enabled: true},
		{ID: FormatElementRef("s1", "e3"), Role: RoleButton, Name: "C", Visible: true, Enabled: true},
	}
	out := RenderElements(els, RenderOptions{MaxNodes: 1})
	if !strings.Contains(out, "2 more nodes not shown; narrow with desktop_find") {
		t.Fatalf("expected truncation notice for 2 skipped nodes, got:\n%s", out)
	}
}
