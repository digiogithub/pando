package windows

import (
	"context"
	"errors"
	"sort"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// defaultFindDepth / defaultFindLimit bound a Find traversal when the caller
// (Scope.Depth / the limit argument) does not specify one, mirroring the
// Linux backend's safety net of the same name — the selector-driven pruning
// in findRec is what actually keeps a real traversal cheap.
const (
	defaultFindDepth = 40
	defaultFindLimit = 200
)

// treeNode is one UIA element as seen by the generic traversal below: an
// opaque, provider-defined id (the real windows backend uses the encoded
// RuntimeId) plus the batch of properties fetched for it. A treeNode never
// carries a live COM pointer — that lives only in the windows-only backend's
// handle table, keyed by the same id.
type treeNode struct {
	id    string
	props cachedProps
}

// childProvider is the minimal surface findRec depends on to walk a tree,
// so the algorithm can be exercised in unit tests against a fake in-memory
// tree (traverse_test.go) with no COM involved at all — mirroring the
// Linux backend's busConn test seam. The real implementation
// (backend_windows.go's uiaChildProvider) fetches one whole tree level in a
// single UIA FindAll(TreeScope_Children, TrueCondition, cacheRequest) call,
// UIA's analogue of AT-SPI's per-object property batching: a single
// cross-process hop returns every child's cached properties together.
type childProvider interface {
	childrenOf(ctx context.Context, id string) ([]treeNode, error)
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
// This mirrors internal/uiauto/platform/linux/traverse.go's findState
// exactly; only the node-fetching side (childProvider vs busConn) differs
// between the two backends.
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

// findRec walks the subtree rooted at node, testing the pending selector
// steps in fs against each node's normalized core.Element, collecting full
// matches into results, and pruning any branch that can no longer possibly
// complete a match. It returns early (without error) once results reaches
// limit, once maxDepth is exceeded, or once no pending step remains for a
// subtree; it returns an error only for ctx cancellation or an
// unrecoverable provider failure at the scope-root node.
func findRec(
	ctx context.Context,
	provider childProvider,
	sel *core.Selector,
	backendName, appID string,
	node treeNode,
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

	el := buildElement(backendName, appID, "", node.props)

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
		// Nothing pending can ever match below this node: prune the branch,
		// saving the cross-process FindAll call entirely.
		return nil
	}

	children, err := provider.childrenOf(ctx, node.id)
	if err != nil {
		if isCtxErr(err) {
			return err
		}
		// A single unreadable node (torn down mid-walk, RPC hiccup, ...)
		// should not abort the whole search; skip this branch.
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
		if err := findRec(ctx, provider, sel, backendName, appID, child, findState{ds: childDS, cs: cs}, level+1, maxDepth, results, limit); err != nil {
			return err
		}
	}
	return nil
}
