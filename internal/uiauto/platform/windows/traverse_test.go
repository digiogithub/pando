package windows

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// fakeProvider is an in-memory childProvider for exercising findRec without
// any COM involved, mirroring the Linux backend's fakeConn test seam.
type fakeProvider struct {
	children map[string][]treeNode
	calls    map[string]int
	err      error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{children: map[string][]treeNode{}, calls: map[string]int{}}
}

func (f *fakeProvider) add(parent string, children ...treeNode) {
	f.children[parent] = append(f.children[parent], children...)
}

func (f *fakeProvider) childrenOf(ctx context.Context, id string) ([]treeNode, error) {
	f.calls[id]++
	if f.err != nil && id == "err" {
		return nil, f.err
	}
	return f.children[id], nil
}

func node(id, name string, ct int32) treeNode {
	return treeNode{id: id, props: cachedProps{RuntimeID: []int32{1}, Name: name, ControlType: ct, Enabled: true}}
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
	p := newFakeProvider()
	p.add("root", node("a", "First", ControlTypeGroup), node("b", "Save", ControlTypeButton))
	p.add("a", node("c", "Nested Save", ControlTypeButton))

	sel := mustSelector(t, `button[name="Save"]`)
	var results []*core.Element
	root := treeNode{id: "root", props: cachedProps{Name: "root", ControlType: ControlTypeWindow, Enabled: true}}
	if err := findRec(context.Background(), p, sel, "uia", "1", root, findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Save" {
		t.Fatalf("expected exactly the top-level Save button, got %+v", results)
	}
}

func TestFindRecChildCombinatorChain(t *testing.T) {
	p := newFakeProvider()
	p.add("root", node("g1", "G1", ControlTypeGroup), node("g2", "G2", ControlTypeGroup))
	p.add("g1", node("b1", "OK", ControlTypeButton))
	p.add("g2", node("b2", "OK", ControlTypeButton))

	sel := mustSelector(t, `group > button[name="OK"]`)
	var results []*core.Element
	root := treeNode{id: "root", props: cachedProps{Name: "root", ControlType: ControlTypeWindow, Enabled: true}}
	if err := findRec(context.Background(), p, sel, "uia", "1", root, findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches (one per group), got %d: %+v", len(results), results)
	}
}

func TestFindRecLimit(t *testing.T) {
	p := newFakeProvider()
	for i := 0; i < 5; i++ {
		p.add("root", node(string(rune('a'+i)), "Item", ControlTypeListItem))
	}
	sel := mustSelector(t, `listitem`)
	var results []*core.Element
	root := treeNode{id: "root", props: cachedProps{ControlType: ControlTypeList}}
	if err := findRec(context.Background(), p, sel, "uia", "", root, findState{ds: []int{0}}, 0, defaultFindDepth, &results, 2); err != nil {
		t.Fatalf("findRec error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected limit=2 to stop early, got %d results", len(results))
	}
}

func TestFindRecMaxDepthPrunesBeforeMatch(t *testing.T) {
	p := newFakeProvider()
	p.add("root", node("a", "A", ControlTypeGroup))
	p.add("a", node("b", "Deep Save", ControlTypeButton))

	sel := mustSelector(t, `button`)
	var results []*core.Element
	root := treeNode{id: "root", props: cachedProps{ControlType: ControlTypeWindow}}
	if err := findRec(context.Background(), p, sel, "uia", "", root, findState{ds: []int{0}}, 0, 1, &results, 0); err != nil {
		t.Fatalf("findRec error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected maxDepth=1 to prevent reaching the button at depth 2, got %+v", results)
	}
}

func TestFindRecBranchPrunedWhenNothingPending(t *testing.T) {
	p := newFakeProvider()
	p.add("root", node("a", "A", ControlTypeGroup))
	sel := mustSelector(t, `group > button`)
	root := treeNode{id: "root", props: cachedProps{ControlType: ControlTypeWindow}}
	var results []*core.Element
	// Seed with only a cs-only findState, nothing pending on the root itself
	// (mirrors the isolated-branch-pruning test in the Linux backend, since
	// the public entry always seeds ds={0}, which — like CSS — never fully
	// exhausts on its own).
	if err := findRec(context.Background(), p, sel, "uia", "", root, findState{}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec error: %v", err)
	}
	if calls := p.calls["root"]; calls != 0 {
		t.Fatalf("expected the branch to be pruned without any childrenOf call, got %d calls", calls)
	}
}

func TestFindRecSkipsUnreadableBranchWithoutAborting(t *testing.T) {
	p := newFakeProvider()
	p.err = errors.New("boom")
	p.add("root", node("err", "Broken", ControlTypeGroup), node("b", "Save", ControlTypeButton))
	p.add("err", node("x", "Unreachable Save", ControlTypeButton))

	sel := mustSelector(t, `button[name="Save"]`)
	var results []*core.Element
	root := treeNode{id: "root", props: cachedProps{ControlType: ControlTypeWindow}}
	if err := findRec(context.Background(), p, sel, "uia", "", root, findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec should not abort on one broken branch: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Save" {
		t.Fatalf("expected the sibling Save button to still be found, got %+v", results)
	}
}

func TestFindRecContextCancellation(t *testing.T) {
	p := newFakeProvider()
	p.add("root", node("a", "A", ControlTypeGroup))
	sel := mustSelector(t, `button`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := treeNode{id: "root", props: cachedProps{ControlType: ControlTypeWindow}}
	var results []*core.Element
	err := findRec(ctx, p, sel, "uia", "", root, findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestFindRecMemoNotRequired(t *testing.T) {
	// Unlike the Linux backend (one round trip per attribute), this
	// traversal fetches a whole batch of children per level via
	// childrenOf, so each parent id is queried at most once per findRec
	// invocation naturally (no shared-branch re-fetch scenario exists at
	// this layer); this test just documents/asserts that expectation for a
	// simple fan-out.
	p := newFakeProvider()
	p.add("root", node("a", "A", ControlTypeGroup), node("b", "B", ControlTypeGroup))
	sel := mustSelector(t, `group`)
	root := treeNode{id: "root", props: cachedProps{ControlType: ControlTypeWindow}}
	var results []*core.Element
	if err := findRec(context.Background(), p, sel, "uia", "", root, findState{ds: []int{0}}, 0, defaultFindDepth, &results, 0); err != nil {
		t.Fatalf("findRec error: %v", err)
	}
	if p.calls["root"] != 1 {
		t.Fatalf("expected exactly one childrenOf(root) call, got %d", p.calls["root"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 group matches, got %d", len(results))
	}
}

func TestFilterNth(t *testing.T) {
	sel := mustSelector(t, `group > button[nth=2]`)
	cs := []int{1}
	if got := filterNth(cs, sel, 0); len(got) != 0 {
		t.Fatalf("filterNth childIndex=0 (position 1) should drop nth=2, got %v", got)
	}
	if got := filterNth(cs, sel, 1); len(got) != 1 {
		t.Fatalf("filterNth childIndex=1 (position 2) should keep nth=2, got %v", got)
	}
}
