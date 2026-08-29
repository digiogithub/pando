package linux

import (
	"context"
	"errors"
	"sort"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// defaultFindDepth / defaultFindLimit bound a Find traversal when the
// caller (Scope.Depth / the limit argument) does not specify one. They
// exist purely as a safety net — the selector-driven pruning in findRec is
// what actually keeps a real traversal cheap.
const (
	defaultFindDepth = 40
	defaultFindLimit = 200
)

// traverseState is the per-call context threaded through a Find/Children
// traversal: the busConn, the backend name to stamp on built elements, and
// a short-lived memo cache so a single traversal never re-fetches the same
// AT-SPI object's properties or child list twice (e.g. because two selector
// steps are pending against the same node, or because a node is on the
// frontier of more than one still-unmatched branch).
type traverseState struct {
	conn        busConn
	backendName string

	nodeCache  map[accessibleRef]*fetchedNode
	childCache map[accessibleRef][]accessibleRef
}

func newTraverseState(conn busConn, backendName string) *traverseState {
	return &traverseState{
		conn:        conn,
		backendName: backendName,
		nodeCache:   make(map[accessibleRef]*fetchedNode),
		childCache:  make(map[accessibleRef][]accessibleRef),
	}
}

func (t *traverseState) node(ctx context.Context, ref accessibleRef) (*fetchedNode, error) {
	if n, ok := t.nodeCache[ref]; ok {
		return n, nil
	}
	n, err := fetchNode(ctx, t.conn, ref)
	if err != nil {
		return nil, err
	}
	t.nodeCache[ref] = n
	return n, nil
}

func (t *traverseState) children(ctx context.Context, ref accessibleRef) ([]accessibleRef, error) {
	if c, ok := t.childCache[ref]; ok {
		return c, nil
	}
	body, err := t.conn.call(ctx, ref.Bus, ref.Path, accessibleIface, "GetChildren")
	if err != nil {
		return nil, err
	}
	var refs []soRef
	if len(body) > 0 {
		if err := storeSoRefSlice(body[0], &refs); err != nil {
			return nil, err
		}
	}
	out := make([]accessibleRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.ref())
	}
	t.childCache[ref] = out
	return out, nil
}

// isCtxErr reports whether err is (or wraps) a context cancellation/
// deadline error, the only kind of traversal error that should abort the
// whole Find/Children call rather than just skipping one branch.
func isCtxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// findState is the small state machine threaded down one DFS branch of a
// selector-driven Find: which selector step indices are still pending
// against every remaining descendant (ds — carried by a "descendant"
// combinator, i.e. plain whitespace) versus only against the immediate
// children of the current node (cs — carried by a child ">" combinator).
type findState struct {
	ds []int
	cs []int
}

func uniqueSortedAppend(s []int, v int) []int {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	s = append(s, v)
	sort.Ints(s)
	return s
}

// findRec walks the subtree rooted at ref, testing the pending selector
// steps in fs against each node, collecting full matches into results, and
// pruning any branch that can no longer possibly complete a match. It
// returns early (without error) once results reaches limit, once maxDepth
// is exceeded, or once no pending step remains for a subtree; it returns an
// error only for ctx cancellation or an unrecoverable connection failure at
// the very first (scope-root) node.
func findRec(
	ctx context.Context,
	t *traverseState,
	sel *core.Selector,
	ref accessibleRef,
	fs findState,
	level, maxDepth int,
	results *[]*core.Element,
	limit int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if limit > 0 && len(*results) >= limit {
		return nil
	}

	n, err := t.node(ctx, ref)
	if err != nil {
		if isCtxErr(err) {
			return err
		}
		// A single unreadable node (defunct, race with app teardown, ...)
		// should not abort the whole search; skip this branch.
		return nil
	}
	el := n.toElement(t.backendName, ref.Bus)

	pending := append(append([]int(nil), fs.ds...), fs.cs...)
	sort.Ints(pending)

	childDS := append([]int(nil), fs.ds...)
	var childCS []int
	lastIdx := len(sel.Steps) - 1

	seen := -1
	for _, idx := range pending {
		if idx == seen {
			continue
		}
		seen = idx
		step := sel.Steps[idx]
		if !step.MatchesElement(el) {
			continue
		}
		if idx == lastIdx {
			*results = append(*results, el)
			if limit > 0 && len(*results) >= limit {
				return nil
			}
			continue
		}
		next := idx + 1
		if sel.Steps[next].Combinator == core.CombinatorChild {
			childCS = uniqueSortedAppend(childCS, next)
		} else {
			childDS = uniqueSortedAppend(childDS, next)
		}
	}

	if level >= maxDepth {
		return nil
	}
	if len(childDS) == 0 && len(childCS) == 0 {
		// Nothing pending can ever match below this node: prune the branch.
		return nil
	}

	children, err := t.children(ctx, ref)
	if err != nil {
		if isCtxErr(err) {
			return err
		}
		return nil
	}

	for i, child := range children {
		if limit > 0 && len(*results) >= limit {
			return nil
		}
		cs := filterNth(childCS, sel, i)
		if len(childDS) == 0 && len(cs) == 0 {
			continue
		}
		if err := findRec(ctx, t, sel, child, findState{ds: childDS, cs: cs}, level+1, maxDepth, results, limit); err != nil {
			return err
		}
	}
	return nil
}

// filterNth drops a child-combinator pending step index from cs when the
// step carries an Nth predicate that does not match childIndex (0-based
// position among this parent's children). Steps without an Nth predicate
// pass through unchanged.
func filterNth(cs []int, sel *core.Selector, childIndex int) []int {
	if len(cs) == 0 {
		return cs
	}
	out := make([]int, 0, len(cs))
	for _, idx := range cs {
		if n := sel.Steps[idx].Nth; n > 0 && n != childIndex+1 {
			continue
		}
		out = append(out, idx)
	}
	return out
}
