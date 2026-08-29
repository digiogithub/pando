package linux

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// buildSampleTree wires up:
//
//	app (application)
//	  window1 (frame, "Main Window")
//	    panel (panel)
//	      btnSave   (push button, "Save")
//	      btnCancel (push button, "Cancel")
//	      textfield (entry, "Search")
func buildSampleTree() *fakeConn {
	conn := newFakeConn()
	app := ref("app1", "/app")
	win := ref("app1", "/win1")
	panel := ref("app1", "/panel")
	save := ref("app1", "/save")
	cancel := ref("app1", "/cancel")
	search := ref("app1", "/search")

	conn.add(&fakeNode{ref: app, roleName: "application", name: "App", children: []accessibleRef{win}})
	conn.add(&fakeNode{ref: win, roleName: "frame", name: "Main Window", children: []accessibleRef{panel},
		state: stateWord(stateVisible, stateShowing)})
	conn.add(&fakeNode{ref: panel, roleName: "panel", name: "", children: []accessibleRef{save, cancel, search}})
	conn.add(&fakeNode{ref: save, roleName: "push button", name: "Save",
		interfaces:  []string{"org.a11y.atspi.Action", "org.a11y.atspi.Component"},
		actionNames: []string{"click"}, state: stateWord(stateEnabled, stateSensitive, stateVisible, stateShowing)})
	conn.add(&fakeNode{ref: cancel, roleName: "push button", name: "Cancel",
		interfaces:  []string{"org.a11y.atspi.Action", "org.a11y.atspi.Component"},
		actionNames: []string{"click"}, state: stateWord(stateEnabled, stateSensitive, stateVisible, stateShowing)})
	conn.add(&fakeNode{ref: search, roleName: "entry", name: "Search",
		interfaces: []string{"org.a11y.atspi.EditableText", "org.a11y.atspi.Component"},
		state:      stateWord(stateEnabled, stateSensitive, stateVisible, stateShowing)})
	return conn
}

func mustSelector(t *testing.T, s string) *core.Selector {
	t.Helper()
	sel, err := core.ParseSelector(s)
	if err != nil {
		t.Fatalf("ParseSelector(%q) failed: %v", s, err)
	}
	return sel
}

func TestFindRecSingleStepDescendant(t *testing.T) {
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `button[name="Save"]`)

	var results []*core.Element
	err := findRec(context.Background(), ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0)
	if err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Save" {
		t.Fatalf("expected exactly one match named Save, got %+v", results)
	}
}

func TestFindRecChildCombinatorChain(t *testing.T) {
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `window > panel > button`)

	var results []*core.Element
	err := findRec(context.Background(), ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0)
	if err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 button matches (Save, Cancel), got %d: %+v", len(results), results)
	}
	names := map[string]bool{results[0].Name: true, results[1].Name: true}
	if !names["Save"] || !names["Cancel"] {
		t.Fatalf("expected Save and Cancel, got %+v", names)
	}
}

func TestFindRecLimitStopsEarly(t *testing.T) {
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `button`)

	var results []*core.Element
	err := findRec(context.Background(), ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, defaultFindDepth, &results, 1)
	if err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected limit=1 to cap results at 1, got %d", len(results))
	}
}

func TestFindRecDepthCapPrunesBeforeMatch(t *testing.T) {
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `window > panel > button`)

	// app(level0) -> window(level1) -> panel(level2) -> button(level3).
	// maxDepth=2 means recursion stops once a node at level>=2 has been
	// visited, so the buttons (level 3) are never reached.
	var results []*core.Element
	err := findRec(context.Background(), ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, 2, &results, 0)
	if err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected depth cap to prevent any match, got %+v", results)
	}

	// maxDepth=3 lets recursion reach the buttons.
	results = nil
	if err := findRec(context.Background(), ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, 3, &results, 0); err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches at maxDepth=3, got %d", len(results))
	}
}

func TestFindRecPrunesWhenNoPendingSteps(t *testing.T) {
	// A plain (unanchored, descendant-combinator) first step keeps its
	// pending state ("ds") alive for every descendant forever — like CSS,
	// it could still match arbitrarily deep. Real pruning happens once a
	// branch is only carrying *child*-combinator ("cs") obligations and
	// those fail to match: nothing can complete the selector below that
	// node, so the branch is dropped without even listing its children.
	// That scenario only arises after a preceding step has already
	// resolved ds down to nothing, which findRec's public entry point
	// (always seeding ds={0}) cannot exercise directly — so this test
	// drives findRec with a synthetic cs-only state to isolate it.
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `nonexistentrole`)

	var results []*core.Element
	appRef := ref("app1", "/app")
	if err := findRec(context.Background(), ts, sel, appRef, findState{cs: []int{0}}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no matches for an unmatchable role, got %+v", results)
	}
	if n := conn.callCount[appRef.String()+":"+accessibleIface+".GetChildren"]; n != 0 {
		t.Fatalf("expected the exhausted cs-only branch to prune before descending, GetChildren called %d times", n)
	}
}

func TestFindRecSkipsUnreadableBranchWithoutAborting(t *testing.T) {
	conn := buildSampleTree()
	conn.failRefs = map[accessibleRef]bool{ref("app1", "/save"): true}
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `button`)

	var results []*core.Element
	if err := findRec(context.Background(), ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec returned error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Cancel" {
		t.Fatalf("expected the unreadable Save branch to be skipped and Cancel still found, got %+v", results)
	}
}

func TestFindRecHonoursContextCancellation(t *testing.T) {
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")
	sel := mustSelector(t, `button`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var results []*core.Element
	err := findRec(ctx, ts, sel, ref("app1", "/app"), findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0)
	if err == nil {
		t.Fatalf("expected findRec to honour an already-cancelled context")
	}
}

func TestFindRecNodeCacheAvoidsRefetch(t *testing.T) {
	conn := buildSampleTree()
	ts := newTraverseState(conn, "atspi")

	winRef := ref("app1", "/win1")
	if _, err := ts.node(context.Background(), winRef); err != nil {
		t.Fatalf("node() failed: %v", err)
	}
	if _, err := ts.node(context.Background(), winRef); err != nil {
		t.Fatalf("node() failed: %v", err)
	}
	if n := conn.callCount[winRef.String()+":GetAll:"+accessibleIface]; n != 1 {
		t.Fatalf("expected exactly 1 GetAll(Accessible) call for a memoized node, got %d", n)
	}
}

func TestFilterNthOnlyKeepsMatchingPosition(t *testing.T) {
	sel := mustSelector(t, `group > button[nth=2]`)
	cs := []int{1}
	if got := filterNth(cs, sel, 0); len(got) != 0 {
		t.Fatalf("expected nth=2 to be dropped for childIndex 0, got %v", got)
	}
	if got := filterNth(cs, sel, 1); len(got) != 1 {
		t.Fatalf("expected nth=2 to be kept for childIndex 1, got %v", got)
	}
}
