package darwin

import (
	"context"
	"errors"
	"sort"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// defaultFindDepth / defaultFindLimit bound a Find traversal when the
// caller does not specify one, mirroring the Linux AT-SPI2 backend
// (Phase 2). The selector-driven pruning in findRec is what actually keeps
// a real traversal cheap.
const (
	defaultFindDepth = 40
	defaultFindLimit = 200
)

// traverseState is the per-call context threaded through a Find/Children
// traversal: the axConn, the backend name to stamp on built elements, and a
// short-lived memo cache so a single traversal never re-fetches the same
// AXUIElement's batched attributes twice.
type traverseState struct {
	conn        axConn
	backendName string

	nodeCache map[axRef]*fetchedNode
}

func newTraverseState(conn axConn, backendName string) *traverseState {
	return &traverseState{conn: conn, backendName: backendName, nodeCache: make(map[axRef]*fetchedNode)}
}

func (t *traverseState) node(ctx context.Context, ref axRef) (*fetchedNode, error) {
	if n, ok := t.nodeCache[ref]; ok {
		return n, nil
	}
	raw, err := t.conn.attributes(ctx, ref, fixedAttrNames)
	if err != nil {
		return nil, err
	}
	n := parseFetchedNode(ref, raw)
	t.nodeCache[ref] = n
	return n, nil
}

func isCtxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// findState is the small state machine threaded down one DFS branch of a
// selector-driven Find, mirroring the Linux backend's design: ds
// (descendant-combinator pending step indices) persists to every depth
// until matched; cs (child-combinator pending indices) applies only to the
// immediate children of the node that satisfied the preceding step.
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

// findRec walks the subtree rooted at ref, testing pending selector steps
// against each node, collecting full matches into results, and pruning any
// branch that can no longer possibly complete a match. path accumulates the
// root-relative child-index chain used for the durable re-resolution key
// (Native.Data[nativeIndexPathKey]).
func findRec(
	ctx context.Context,
	t *traverseState,
	sel *core.Selector,
	ref axRef,
	fs findState,
	path []int,
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
		// A single unreadable node (defunct, torn down mid-search, ...)
		// should not abort the whole search; skip this branch.
		return nil
	}
	el := n.toElement(t.backendName, ref.PID, path)

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

	for i, child := range n.children {
		if limit > 0 && len(*results) >= limit {
			return nil
		}
		cs := filterNth(childCS, sel, i)
		if len(childDS) == 0 && len(cs) == 0 {
			continue
		}
		childPath := append(append([]int(nil), path...), i)
		if err := findRec(ctx, t, sel, child, findState{ds: childDS, cs: cs}, childPath, level+1, maxDepth, results, limit); err != nil {
			return err
		}
	}
	return nil
}

// filterNth drops a child-combinator pending step index from cs when the
// step carries an Nth predicate that does not match childIndex (0-based
// position among this parent's AXChildren). Steps without an Nth predicate
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
