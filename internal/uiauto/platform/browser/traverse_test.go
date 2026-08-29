package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// buildTestTree wires up a small fake page:
//
//	root (window)
//	  group
//	    button "Save"
//	    button "Cancel"
//	  textField "Search"
func buildTestTree(f *fakeConn, targetID string) {
	root := &accessibility.Node{NodeID: "1", Role: axVal("WebArea"), Name: axVal("Doc")}
	group := &accessibility.Node{NodeID: "2", Role: axVal("group")}
	save := &accessibility.Node{NodeID: "3", Role: axVal("button"), Name: axVal("Save"), BackendDOMNodeID: 30}
	cancel := &accessibility.Node{NodeID: "4", Role: axVal("button"), Name: axVal("Cancel"), BackendDOMNodeID: 40}
	search := &accessibility.Node{NodeID: "5", Role: axVal("textField"), Name: axVal("Search"), BackendDOMNodeID: 50}

	tid := targetIDFor(targetID)
	f.addNode(tid, root, "")
	f.addNode(tid, group, root.NodeID)
	f.addNode(tid, save, group.NodeID)
	f.addNode(tid, cancel, group.NodeID)
	f.addNode(tid, search, root.NodeID)
}

func TestFindDescendantMatch(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	withActiveSession(t)

	sel, err := core.ParseSelector(`button[name="Save"]`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := b.Find(context.Background(), core.Scope{WindowID: "T1"}, sel, 0)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Save" {
		t.Fatalf("results = %+v", results)
	}
}

func TestFindChildCombinatorChain(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	withActiveSession(t)

	sel, err := core.ParseSelector(`group > button`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := b.Find(context.Background(), core.Scope{WindowID: "T1"}, sel, 0)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 buttons", results)
	}
}

func TestFindLimitStopsEarly(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	withActiveSession(t)

	sel, err := core.ParseSelector(`button`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := b.Find(context.Background(), core.Scope{WindowID: "T1"}, sel, 1)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want exactly 1 (limit)", results)
	}
}

func TestFindMaxDepthPrunesBeforeMatch(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	withActiveSession(t)

	sel, err := core.ParseSelector(`button[name="Save"]`)
	if err != nil {
		t.Fatal(err)
	}
	// root(depth0) -> group(depth1) -> button(depth2): a maxDepth of 1
	// should never reach the button.
	results, err := b.Find(context.Background(), core.Scope{WindowID: "T1", Depth: 1}, sel, 0)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none (depth-pruned)", results)
	}
}

func TestFindSkipsUnreadableBranchWithoutAborting(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	tid := targetIDFor("T1")
	f.failChildren[tid] = map[accessibility.NodeID]error{
		"2": errors.New("simulated: node detached"),
	}
	b := newBackendWithConn(f)
	withActiveSession(t)

	sel, err := core.ParseSelector(`textField`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := b.Find(context.Background(), core.Scope{WindowID: "T1"}, sel, 0)
	if err != nil {
		t.Fatalf("Find should not abort on one unreadable branch: %v", err)
	}
	if len(results) != 1 || results[0].Name != "Search" {
		t.Fatalf("results = %+v", results)
	}
}

func TestFindCtxCancellationAborts(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	withActiveSession(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sel, _ := core.ParseSelector(`button`)
	_, err := b.Find(ctx, core.Scope{WindowID: "T1"}, sel, 0)
	if err == nil {
		t.Fatal("expected a context-cancellation error")
	}
}

func TestFindNoActiveSessionReturnsAppNotFound(t *testing.T) {
	f := newFakeConn()
	buildTestTree(f, "T1")
	b := newBackendWithConn(f)
	// Deliberately no withActiveSession(t) here.

	sel, _ := core.ParseSelector(`button`)
	_, err := b.Find(context.Background(), core.Scope{WindowID: "T1"}, sel, 0)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrAppNotFound {
		t.Fatalf("err = %v, want APP_NOT_FOUND", err)
	}
}

func TestFilterNthChildCombinator(t *testing.T) {
	sel, err := core.ParseSelector(`group > button[nth=2]`)
	if err != nil {
		t.Fatal(err)
	}
	cs := []int{1}
	if out := filterNth(cs, sel, 0); len(out) != 0 {
		t.Fatalf("index 0 (child 1) should be filtered out for nth=2, got %v", out)
	}
	if out := filterNth(cs, sel, 1); len(out) != 1 {
		t.Fatalf("index 1 (child 2) should match nth=2, got %v", out)
	}
}

// withActiveSession registers a background session for the duration of the
// test so ActiveSession() reports available without touching a real
// browser, and unregisters it on cleanup.
func withActiveSession(t *testing.T) {
	t.Helper()
	id := "test-session-" + t.Name()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	RegisterSession(id, ctx)
	t.Cleanup(func() {
		cancel()
		UnregisterSession(id)
	})
}
