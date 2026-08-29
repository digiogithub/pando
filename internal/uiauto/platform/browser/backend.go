package browser

import (
	"context"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/target"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// CdpBackend implements core.Backend by serving Element data from the
// Chrome DevTools Protocol Accessibility/DOM domains of an already-running
// browser session opened by the browser_* agent tools. It never launches a
// browser itself: Available/Apps/Windows/Find/Children all report honestly
// when no session is registered, rather than spawning one, so resolving
// "auto" (which includes "cdp" in its preference order) can never launch
// Chrome as a side effect.
type CdpBackend struct {
	conn axConn
}

// NewBackend constructs a CdpBackend. It never fails and never launches a
// browser: it only wires up the axConn that reads whatever session
// RegisterSession has published.
func NewBackend() (core.Backend, error) {
	return &CdpBackend{conn: newLiveConn()}, nil
}

// newBackendWithConn is the test seam: it builds a CdpBackend around an
// injected axConn (typically a fake) instead of the real liveConn.
func newBackendWithConn(conn axConn) *CdpBackend {
	return &CdpBackend{conn: conn}
}

// Name implements core.Backend.
func (b *CdpBackend) Name() string { return "cdp" }

// Available implements core.Backend. It never launches a browser: absent a
// registered session it reports an all-false Capabilities plus an
// APP_NOT_FOUND DesktopError suggesting the agent open a page with
// browser_navigate first (Manager tolerates Available erroring and simply
// degrades to empty Capabilities, per its documented contract). A
// registered-but-unreachable session (e.g. the tab was closed underneath
// us) degrades the same way.
func (b *CdpBackend) Available(ctx context.Context) (core.Capabilities, error) {
	if _, ok := ActiveSession(); !ok {
		return core.Capabilities{}, errNoActiveSession
	}
	if _, err := b.conn.Version(ctx); err != nil {
		return core.Capabilities{}, core.NewAppNotFoundError(
			"a browser session is registered but not reachable over CDP: " + err.Error())
	}
	return core.Capabilities{
		Accessibility: true,
		UIInspection:  true,
		UIActions:     true,
		// CdpBackend implements events.Subscriber over real CDP DOM/
		// Accessibility domain events (events.go) whenever a session is
		// registered and reachable, which is exactly the condition
		// already checked above.
		Events: true,
	}, nil
}

// Close implements core.Backend: it releases this backend's own per-target
// attachments, never the externally-owned registered browser session.
func (b *CdpBackend) Close() error { return b.conn.Close() }

// Apps implements core.Backend: the connected browser is reported as a
// single application (see appID in element.go), named from its CDP
// Browser.getVersion product string.
func (b *CdpBackend) Apps(ctx context.Context) ([]core.AppInfo, error) {
	if _, ok := ActiveSession(); !ok {
		return nil, errNoActiveSession
	}
	product, err := b.conn.Version(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	targets, err := b.conn.Targets(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	windows := 0
	for _, t := range targets {
		if t.Type == "page" {
			windows++
		}
	}
	name := strings.TrimSpace(product)
	if name == "" {
		name = "Browser"
	}
	return []core.AppInfo{{ID: appID, Name: name, Windows: windows}}, nil
}

// Windows implements core.Backend: every CDP target of type "page" is
// reported as a window, keyed by its target id.
func (b *CdpBackend) Windows(ctx context.Context, appIDFilter string) ([]core.WindowInfo, error) {
	if _, ok := ActiveSession(); !ok {
		return nil, errNoActiveSession
	}
	if appIDFilter != "" && !strings.EqualFold(appIDFilter, appID) {
		return nil, core.NewAppNotFoundError("no running application matches app id/name " + appIDFilter)
	}
	targets, err := b.conn.Targets(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	var out []core.WindowInfo
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		title := strings.TrimSpace(t.Title)
		if title == "" {
			title = t.URL
		}
		out = append(out, core.WindowInfo{
			ID:      string(t.TargetID),
			AppID:   appID,
			Title:   title,
			Focused: t.Attached,
		})
	}
	return out, nil
}

// scopeRoot is one starting point for a Find traversal: a normalized
// Element (for matching against the selector's first step) plus the raw
// (targetID, nodeID) pair traverse.go needs to descend from it.
type scopeRoot struct {
	el       *core.Element
	targetID target.ID
	nodeID   accessibility.NodeID
}

// resolveScopeRoots resolves the starting scopeRoot(s) for a Find call from
// scope: scope.Root when set, the named window's root AX node when
// scope.WindowID is set, or the root AX node of every "page" target when
// scope carries no window context at all (still selector-pruned/limit/
// depth-capped by findRec, never a full-tree walk).
func (b *CdpBackend) resolveScopeRoots(ctx context.Context, scope core.Scope) ([]scopeRoot, error) {
	if scope.Root != nil {
		targetID, axNodeID, hasAX, err := refFromElement(scope.Root)
		if err != nil {
			return nil, err
		}
		if hasAX {
			return []scopeRoot{{el: scope.Root, targetID: targetID, nodeID: axNodeID}}, nil
		}
		root, err := b.conn.RootNode(ctx, targetID)
		if err != nil {
			return nil, mapErr(err)
		}
		return []scopeRoot{{el: toElement(root, targetID), targetID: targetID, nodeID: root.NodeID}}, nil
	}
	if scope.WindowID != "" {
		targetID := target.ID(scope.WindowID)
		root, err := b.conn.RootNode(ctx, targetID)
		if err != nil {
			return nil, mapErr(err)
		}
		return []scopeRoot{{el: toElement(root, targetID), targetID: targetID, nodeID: root.NodeID}}, nil
	}
	if scope.AppID != "" && !strings.EqualFold(scope.AppID, appID) {
		return nil, core.NewAppNotFoundError("no running application matches app id/name " + scope.AppID)
	}
	targets, err := b.conn.Targets(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	var roots []scopeRoot
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		root, err := b.conn.RootNode(ctx, t.TargetID)
		if err != nil {
			continue // skip a page that vanished/errored mid-listing
		}
		roots = append(roots, scopeRoot{el: toElement(root, t.TargetID), targetID: t.TargetID, nodeID: root.NodeID})
	}
	return roots, nil
}

// Find implements core.Backend with the selector-driven, depth-capped,
// limit-capped, ctx-aware traversal in traverse.go -- it never walks a
// whole page's tree.
func (b *CdpBackend) Find(ctx context.Context, scope core.Scope, sel *core.Selector, limit int) ([]*core.Element, error) {
	if _, ok := ActiveSession(); !ok {
		return nil, errNoActiveSession
	}
	if limit <= 0 {
		limit = defaultFindLimit
	}
	maxDepth := scope.Depth
	if maxDepth <= 0 {
		maxDepth = defaultFindDepth
	}

	roots, err := b.resolveScopeRoots(ctx, scope)
	if err != nil {
		return nil, err
	}

	var results []*core.Element
	// One visited set spans every scope root: the same AX node can be
	// reachable from more than one root as well as from more than one path
	// below a single root (see findRec).
	visited := make(map[accessibility.NodeID]bool)
	for _, root := range roots {
		if limit > 0 && len(results) >= limit {
			break
		}
		if err := findRec(ctx, b.conn, root.targetID, sel, root.el, root.nodeID, findState{ds: []int{0}}, 0, maxDepth, &results, limit, visited); err != nil {
			return nil, mapErr(err)
		}
	}
	return results, nil
}

// Children implements core.Backend: the direct AX children of el, fetched
// with a single Accessibility.getChildAXNodes call.
func (b *CdpBackend) Children(ctx context.Context, el *core.Element) ([]*core.Element, error) {
	targetID, axNodeID, hasAX, err := refFromElement(el)
	if err != nil {
		return nil, err
	}
	if !hasAX {
		root, err := b.conn.RootNode(ctx, targetID)
		if err != nil {
			return nil, mapErr(err)
		}
		axNodeID = root.NodeID
	}
	children, err := b.conn.ChildNodes(ctx, targetID, axNodeID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*core.Element, 0, len(children))
	for _, c := range children {
		out = append(out, toElement(c, targetID))
	}
	return out, nil
}

// boundsFromBoxModel computes the bounding rectangle of a DOM box model's
// content quad (falling back to the border quad when content is empty,
// e.g. a zero-size or replaced element).
func boundsFromBoxModel(m *dom.BoxModel) core.Bounds {
	if m == nil {
		return core.Bounds{}
	}
	q := m.Content
	if len(q) < 8 {
		q = m.Border
	}
	if len(q) < 8 {
		return core.Bounds{}
	}
	minX, maxX := q[0], q[0]
	minY, maxY := q[1], q[1]
	for i := 0; i < len(q); i += 2 {
		x, y := q[i], q[i+1]
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	return core.Bounds{X: int(minX), Y: int(minY), W: int(maxX - minX), H: int(maxY - minY)}
}

// Properties implements core.Backend. The cheap default (props empty)
// returns whatever was already decoded into Native.Data by toElement, plus
// "bounds" is fetched (an extra dom.GetBoxModel round trip) only on
// explicit request, matching the on-demand pattern the other backends use
// for their expensive extras (AT-SPI's "text"/"actions", for example).
func (b *CdpBackend) Properties(ctx context.Context, el *core.Element, props []string) (map[string]any, error) {
	_, targetID, hasAX := elementNativeCoords(el)
	if !hasAX {
		return nil, core.NewElementNotFoundError("element does not carry a CDP accessibility node id; re-observe or re-find it")
	}
	out := make(map[string]any, len(el.Native.Data)+1)
	for k, v := range el.Native.Data {
		out[k] = v
	}

	want := func(name string) bool {
		for _, p := range props {
			if strings.EqualFold(p, name) {
				return true
			}
		}
		return false
	}

	backendID, hasBackend := backendNodeIDFromElement(el)
	if hasBackend && (len(props) == 0 || want("bounds")) {
		if model, err := b.conn.BoxModel(ctx, targetID, backendID); err == nil && model != nil {
			out["bounds"] = boundsFromBoxModel(model)
		}
	}
	if hasBackend && want("tree") {
		if nodes, err := b.conn.PartialTree(ctx, targetID, backendID); err == nil {
			out["axSubtree"] = nodes
		}
	}
	return out, nil
}

// elementNativeCoords is a small refFromElement wrapper for Properties,
// which does not need the AX nodeId itself.
func elementNativeCoords(el *core.Element) (accessibility.NodeID, target.ID, bool) {
	targetID, axNodeID, hasAX, err := refFromElement(el)
	if err != nil {
		return "", "", false
	}
	return axNodeID, targetID, hasAX
}

// Perform implements core.Backend, preferring semantic/DOM actions over
// synthetic mouse input: dom.Focus for focus, a real chromedp click on the
// resolved node for invoke/toggle/select/expand/collapse, dom.SetValue /
// Input.insertText for setvalue/type, and ScrollIntoView (+ dispatch) for
// scroll. An action this backend cannot express returns ACTION_FAILED so
// core.ActionResolver falls back to the Phase 3 physical layer.
func (b *CdpBackend) Perform(ctx context.Context, el *core.Element, action core.Action) error {
	targetID, _, hasAX, err := refFromElement(el)
	if err != nil {
		return err
	}
	if !hasAX {
		return core.NewElementNotFoundError("element does not carry a CDP accessibility node id; re-observe or re-find it")
	}
	backendID, hasBackend := backendNodeIDFromElement(el)
	if !hasBackend {
		return core.NewActionFailedError("element has no backing DOM node to act on")
	}

	// Opportunistically populate Bounds for core.ActionResolver's physical-
	// input fallback path, fetched only now -- for an element that is
	// actually about to be acted on -- never during traversal.
	if el.Bounds.Empty() {
		if model, err := b.conn.BoxModel(ctx, targetID, backendID); err == nil && model != nil {
			el.Bounds = boundsFromBoxModel(model)
		}
	}

	switch action.Kind {
	case core.ActionFocus:
		if err := b.conn.Focus(ctx, targetID, backendID); err != nil {
			return core.NewActionFailedError("cdp focus failed: " + err.Error())
		}
		return nil
	case core.ActionInvoke, core.ActionToggle, core.ActionSelect, core.ActionExpand, core.ActionCollapse:
		if err := b.conn.Click(ctx, targetID, backendID); err != nil {
			return core.NewActionFailedError("cdp click failed: " + err.Error())
		}
		return nil
	case core.ActionSetValue:
		if err := b.conn.SetValue(ctx, targetID, backendID, action.Text); err != nil {
			return core.NewActionFailedError("cdp setvalue failed: " + err.Error())
		}
		return nil
	case core.ActionType:
		if err := b.conn.InsertText(ctx, targetID, backendID, action.Text); err != nil {
			return core.NewActionFailedError("cdp insert text failed: " + err.Error())
		}
		return nil
	case core.ActionScroll:
		if err := b.conn.ScrollIntoView(ctx, targetID, backendID); err != nil {
			return core.NewActionFailedError("cdp scroll failed: " + err.Error())
		}
		return nil
	default:
		return core.NewPlatformNotSupportedError("action " + string(action.Kind) + " is not supported by the CDP backend")
	}
}

// mapErr wraps a plain axConn error into an ACTION_FAILED DesktopError,
// passing an already-structured DesktopError or a context error through
// unchanged.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := core.AsDesktopError(err); ok {
		return err
	}
	if isCtxErr(err) {
		return err
	}
	return core.NewActionFailedError(err.Error())
}
