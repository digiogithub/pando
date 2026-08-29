package darwin

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// buildSimpleTree wires:
//
//	root (window)
//	  |- child0 (button "Save")
//	  |- child1 (button "Cancel")
//	  |- child2 (group)
//	        |- grand0 (button "Nested")
func buildSimpleTree(pid int32) (*fakeAXConn, axRef) {
	c := newFakeAXConn()
	root := nodeRef(pid, 1)
	child0 := nodeRef(pid, 2)
	child1 := nodeRef(pid, 3)
	child2 := nodeRef(pid, 4)
	grand0 := nodeRef(pid, 5)

	c.addNode(root, map[string]any{
		"AXRole": "AXWindow", "AXTitle": "Main",
		"AXChildren": []axRef{child0, child1, child2},
	})
	c.addNode(child0, map[string]any{"AXRole": "AXButton", "AXTitle": "Save", "AXEnabled": true})
	c.addNode(child1, map[string]any{"AXRole": "AXButton", "AXTitle": "Cancel", "AXEnabled": true})
	c.addNode(child2, map[string]any{
		"AXRole": "AXGroup", "AXChildren": []axRef{grand0},
	})
	c.addNode(grand0, map[string]any{"AXRole": "AXButton", "AXTitle": "Nested", "AXEnabled": true})
	return c, root
}

func mustSelector(t *testing.T, s string) *core.Selector {
	t.Helper()
	sel, err := core.ParseSelector(s)
	if err != nil {
		t.Fatalf("ParseSelector(%q): %v", s, err)
	}
	return sel
}

func TestFindRecDescendantMatch(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `button[name="Nested"]`)
	tState := newTraverseState(conn, "ax")
	var results []*core.Element
	if err := findRec(context.Background(), tState, sel, root, findState{ds: []int{0}}, nil, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Nested" {
		t.Fatalf("expected one match Nested, got %+v", results)
	}
}

func TestFindRecChildCombinatorChain(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `window > button`)
	tState := newTraverseState(conn, "ax")
	var results []*core.Element
	if err := findRec(context.Background(), tState, sel, root, findState{ds: []int{0}}, nil, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec: %v", err)
	}
	// Only the two direct button children match "window > button"; the
	// nested button inside the group is two levels down, not a direct
	// child, so it must NOT match.
	if len(results) != 2 {
		t.Fatalf("expected 2 direct-child button matches, got %d: %+v", len(results), results)
	}
}

func TestFindRecLimitStopsEarly(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `button`)
	tState := newTraverseState(conn, "ax")
	var results []*core.Element
	if err := findRec(context.Background(), tState, sel, root, findState{ds: []int{0}}, nil, 0, defaultFindDepth, &results, 1); err != nil {
		t.Fatalf("findRec: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result due to limit, got %d", len(results))
	}
}

func TestFindRecMaxDepthPrunes(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `button[name="Nested"]`)
	tState := newTraverseState(conn, "ax")
	var results []*core.Element
	// Nested is at level 2 (root=0, group=1, grand0=2); maxDepth=1 must
	// not reach it.
	if err := findRec(context.Background(), tState, sel, root, findState{ds: []int{0}}, nil, 0, 1, &results, 0); err != nil {
		t.Fatalf("findRec: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected maxDepth to prune before the match, got %+v", results)
	}
}

func TestFindRecBranchPrunedWhenNothingPending(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `button[name="Save"] > label`) // 2-step selector
	tState := newTraverseState(conn, "ax")
	// Seed with only a child-combinator step pending on a node that does
	// NOT match step 0 ("button[name=Save]") — isolated cs-only findState,
	// since the public entry always seeds ds={0} (mirrors the Linux test).
	var results []*core.Element
	if err := findRec(context.Background(), tState, sel, root, findState{cs: []int{1}}, nil, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected branch to be pruned (nothing pending matches root), got %+v", results)
	}
	// root itself was fetched once (to test the cs-only step against it,
	// which fails, and then further descent is fully pruned since childCS
	// stays empty for a `label` role root does not have and childDS never
	// gets seeded).
	if calls := conn.attrCalls[root]; calls != 1 {
		t.Fatalf("expected exactly 1 attribute fetch for root, got %d", calls)
	}
}

func TestFindRecSkipsOneUnreadableBranch(t *testing.T) {
	conn, root := buildSimpleTree(100)
	// Corrupt one child so fetching it errors; the rest of the tree must
	// still be searched successfully.
	delete(conn.nodes, nodeRef(100, 2))
	sel := mustSelector(t, `button`)
	tState := newTraverseState(conn, "ax")
	var results []*core.Element
	if err := findRec(context.Background(), tState, sel, root, findState{ds: []int{0}}, nil, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec should not abort on one bad branch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 remaining buttons (Cancel, Nested), got %d: %+v", len(results), results)
	}
}

func TestFindRecContextCancellation(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `button`)
	tState := newTraverseState(conn, "ax")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var results []*core.Element
	err := findRec(ctx, tState, sel, root, findState{ds: []int{0}}, nil, 0, defaultFindDepth, &results, 0)
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestFindRecMemoCache(t *testing.T) {
	conn, root := buildSimpleTree(100)
	sel := mustSelector(t, `button`)
	tState := newTraverseState(conn, "ax")
	var results []*core.Element
	if err := findRec(context.Background(), tState, sel, root, findState{ds: []int{0}}, nil, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec: %v", err)
	}
	for ref, calls := range conn.attrCalls {
		if calls != 1 {
			t.Fatalf("expected exactly 1 attribute fetch per node, got %d for %s", calls, ref)
		}
	}
}

func TestFilterNth(t *testing.T) {
	sel := mustSelector(t, `parent > child[nth=2]`)
	cs := []int{1}
	if got := filterNth(cs, sel, 0); len(got) != 0 {
		t.Fatalf("index 0 (nth=1) should be filtered out for nth=2, got %v", got)
	}
	if got := filterNth(cs, sel, 1); len(got) != 1 {
		t.Fatalf("index 1 (nth=2) should pass, got %v", got)
	}
}

func TestIsCtxErr(t *testing.T) {
	if !isCtxErr(context.Canceled) {
		t.Fatalf("expected context.Canceled to be recognized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if !isCtxErr(ctx.Err()) {
		t.Fatalf("expected deadline exceeded to be recognized")
	}
	if isCtxErr(nil) {
		t.Fatalf("nil should not be a context error")
	}
}
