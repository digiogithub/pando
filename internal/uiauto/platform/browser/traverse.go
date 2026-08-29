package browser

import (
	"context"
	"sort"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/target"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// defaultFindDepth / defaultFindLimit bound a Find traversal when the
// caller (Scope.Depth / the limit argument) does not specify one. They
// exist purely as a safety net -- the selector-driven pruning in findRec is
// what actually keeps a real traversal cheap by using
// Accessibility.getChildAXNodes for incremental descent instead of ever
// pulling a full tree.
const (
	defaultFindDepth = 40
	defaultFindLimit = 200
)

// findState is the small state machine threaded down one DFS branch of a
// selector-driven Find: which selector step indices are still pending
// against every remaining descendant (ds -- carried by a "descendant"
// combinator, i.e. plain whitespace) versus only against the immediate
// children of the current node (cs -- carried by a child ">" combinator).
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

// findRec walks the subtree rooted at (targetID, nodeID) -- already
// normalized as el -- testing the pending selector steps in fs against el,
// collecting full matches into results, and pruning any branch that can no
// longer possibly complete a match. It returns early (without error) once
// results reaches limit, once maxDepth is exceeded, or once no pending step
// remains for a subtree; it returns an error only for ctx cancellation or
// an unrecoverable connection failure.
//
// visited guards against Chrome exposing the same AX node through more than
// one path, which is normal rather than exceptional: Accessibility.
// getChildAXNodes on a parent returns both its ignored wrapper nodes AND
// the unignored descendants those wrappers flatten to, so a plain DFS
// reaches the same subtree twice and reports every match in it twice. It is
// keyed by AX node id per Find call, so a node is walked at most once.
func findRec(
	ctx context.Context,
	conn axConn,
	targetID target.ID,
	sel *core.Selector,
	el *core.Element,
	nodeID accessibility.NodeID,
	fs findState,
	level, maxDepth int,
	results *[]*core.Element,
	limit int,
	visited map[accessibility.NodeID]bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if limit > 0 && len(*results) >= limit {
		return nil
	}
	if visited[nodeID] {
		return nil
	}
	visited[nodeID] = true

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

	children, err := conn.ChildNodes(ctx, targetID, nodeID)
	if err != nil {
		if isCtxErr(err) {
			return err
		}
		// A single unreadable node (detached, race with page navigation,
		// ...) should not abort the whole search; skip this branch.
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
		childEl := toElement(child, targetID)
		if err := findRec(ctx, conn, targetID, sel, childEl, child.NodeID, findState{ds: childDS, cs: cs}, level+1, maxDepth, results, limit, visited); err != nil {
			return err
		}
	}
	return nil
}
