package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/accessibility"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

// axConn is the minimal CDP surface the backend/traversal code in this
// package depends on, so tests can substitute a fake in-memory browser
// instead of a real Chrome/Chromium instance.
type axConn interface {
	// Version returns the connected browser's product string (e.g.
	// "Chrome/120.0.6099.109").
	Version(ctx context.Context) (product string, err error)
	// Targets lists every CDP target of the connected browser.
	Targets(ctx context.Context) ([]*target.Info, error)
	// RootNode returns the accessibility root AXNode for targetID's page.
	RootNode(ctx context.Context, targetID target.ID) (*accessibility.Node, error)
	// ChildNodes returns the direct AX children of nodeID within targetID.
	ChildNodes(ctx context.Context, targetID target.ID, nodeID accessibility.NodeID) ([]*accessibility.Node, error)
	// PartialTree re-fetches a single AXNode (no relatives) by its backend
	// DOM node id, used to refresh/verify an element on demand.
	PartialTree(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) ([]*accessibility.Node, error)
	// BoxModel fetches the DOM box model (for Bounds) of a backend DOM
	// node id. Callers are expected to call this lazily -- only for
	// elements about to be acted on, or on explicit request -- never for
	// every node of a traversal.
	BoxModel(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) (*dom.BoxModel, error)
	// Focus, Click, SetValue, InsertText and ScrollIntoView act on a
	// backend DOM node id within targetID.
	Focus(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error
	Click(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error
	SetValue(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID, text string) error
	InsertText(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID, text string) error
	ScrollIntoView(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error
	// Close releases any per-target attachments this conn created. It must
	// never close the underlying, externally-owned browser session.
	Close() error
}

// errNoActiveSession signals no browser session is currently registered by
// the browser_* agent tools; the CDP backend never launches one itself.
var errNoActiveSession = core.NewAppNotFoundError(
	"no browser session is currently open; use the browser_navigate tool to open a page first, then retry")

// isCtxErr reports whether err is (or wraps) a context cancellation/
// deadline error, the only kind of error that should abort a whole
// traversal rather than just skipping one branch.
func isCtxErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// mergeCancel derives a context bound to life's execution machinery (a
// chromedp context, which carries the Executor cdproto calls need) whose
// cancellation additionally follows parent's Done channel/deadline. It lets
// every CDP call here honor the caller's ctx (timeouts, cancellation)
// without needing parent itself to be a chromedp context.
func mergeCancel(parent, life context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(life)
	stop := make(chan struct{})
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-stop:
		}
	}()
	return ctx, func() {
		close(stop)
		cancel()
	}
}

// liveConn is the real axConn implementation, backed by the chromedp
// session registered via RegisterSession. It never owns a browser process:
// it only attaches additional chromedp contexts to existing CDP targets of
// an already-running browser.
type liveConn struct {
	mu      sync.Mutex
	targets map[target.ID]context.Context
	cancels map[target.ID]context.CancelFunc
	enabled map[target.ID]bool
}

func newLiveConn() *liveConn {
	return &liveConn{
		targets: make(map[target.ID]context.Context),
		cancels: make(map[target.ID]context.CancelFunc),
		enabled: make(map[target.ID]bool),
	}
}

// browserContext merges callerCtx with the registered session context for
// browser-scoped (non-target-specific) calls such as Target.getTargets or
// Browser.getVersion. It never creates a browser.
func (c *liveConn) browserContext(callerCtx context.Context) (context.Context, context.CancelFunc, error) {
	sessCtx, ok := ActiveSession()
	if !ok {
		return nil, nil, errNoActiveSession
	}
	ctx, cancel := mergeCancel(callerCtx, sessCtx)
	return ctx, cancel, nil
}

// targetContext returns a chromedp context attached to targetID, merged
// with callerCtx's cancellation. It attaches lazily (once) and caches the
// attachment for reuse; a stale/canceled cached attachment is replaced
// transparently.
func (c *liveConn) targetContext(callerCtx context.Context, targetID target.ID) (context.Context, context.CancelFunc, error) {
	sessCtx, ok := ActiveSession()
	if !ok {
		return nil, nil, errNoActiveSession
	}

	c.mu.Lock()
	tc, cached := c.targets[targetID]
	if cached && tc.Err() != nil {
		cached = false
	}
	c.mu.Unlock()

	if !cached {
		newCtx, cancel := chromedp.NewContext(sessCtx, chromedp.WithTargetID(targetID))
		if err := chromedp.Run(newCtx); err != nil {
			cancel()
			return nil, nil, fmt.Errorf("attach to browser target %s: %w", targetID, err)
		}
		c.mu.Lock()
		if oldCancel, had := c.cancels[targetID]; had {
			oldCancel()
		}
		c.targets[targetID] = newCtx
		c.cancels[targetID] = cancel
		c.enabled[targetID] = false
		c.mu.Unlock()
		tc = newCtx
	}

	if err := c.ensureAccessibilityEnabled(callerCtx, targetID, tc); err != nil {
		return nil, nil, err
	}

	ctx, cancel := mergeCancel(callerCtx, tc)
	return ctx, cancel, nil
}

// ensureAccessibilityEnabled enables the CDP Accessibility domain on
// targetID's attached context, once, so AXNodeIds stay stable across calls.
func (c *liveConn) ensureAccessibilityEnabled(callerCtx context.Context, targetID target.ID, tc context.Context) error {
	c.mu.Lock()
	already := c.enabled[targetID]
	c.mu.Unlock()
	if already {
		return nil
	}

	ctx, cancel := mergeCancel(callerCtx, tc)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		return accessibility.Enable().Do(actx)
	})); err != nil {
		return fmt.Errorf("enable CDP accessibility domain on target %s: %w", targetID, err)
	}

	c.mu.Lock()
	c.enabled[targetID] = true
	c.mu.Unlock()
	return nil
}

// Version implements axConn.
func (c *liveConn) Version(callerCtx context.Context) (string, error) {
	ctx, cancel, err := c.browserContext(callerCtx)
	if err != nil {
		return "", err
	}
	defer cancel()
	var product string
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		_, p, _, _, _, e := cdpbrowser.GetVersion().Do(actx)
		product = p
		return e
	}))
	if err != nil {
		return "", fmt.Errorf("browser.getVersion: %w", err)
	}
	return product, nil
}

// Targets implements axConn.
func (c *liveConn) Targets(callerCtx context.Context) ([]*target.Info, error) {
	ctx, cancel, err := c.browserContext(callerCtx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var infos []*target.Info
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		var e error
		infos, e = target.GetTargets().Do(actx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("target.getTargets: %w", err)
	}
	return infos, nil
}

// RootNode implements axConn.
func (c *liveConn) RootNode(callerCtx context.Context, targetID target.ID) (*accessibility.Node, error) {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var node *accessibility.Node
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		var e error
		node, e = accessibility.GetRootAXNode().Do(actx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("accessibility.getRootAXNode: %w", err)
	}
	return node, nil
}

// ChildNodes implements axConn.
func (c *liveConn) ChildNodes(callerCtx context.Context, targetID target.ID, nodeID accessibility.NodeID) ([]*accessibility.Node, error) {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var nodes []*accessibility.Node
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		var e error
		nodes, e = accessibility.GetChildAXNodes(nodeID).Do(actx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("accessibility.getChildAXNodes: %w", err)
	}
	return nodes, nil
}

// PartialTree implements axConn.
func (c *liveConn) PartialTree(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID) ([]*accessibility.Node, error) {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var nodes []*accessibility.Node
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		var e error
		nodes, e = accessibility.GetPartialAXTree().WithBackendNodeID(backendID).WithFetchRelatives(false).Do(actx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("accessibility.getPartialAXTree: %w", err)
	}
	return nodes, nil
}

// BoxModel implements axConn.
func (c *liveConn) BoxModel(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID) (*dom.BoxModel, error) {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var model *dom.BoxModel
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		var e error
		model, e = dom.GetBoxModel().WithBackendNodeID(backendID).Do(actx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("dom.getBoxModel: %w", err)
	}
	return model, nil
}

// Focus implements axConn.
func (c *liveConn) Focus(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return err
	}
	defer cancel()
	return chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		return dom.Focus().WithBackendNodeID(backendID).Do(actx)
	}))
}

// resolveNodeID converts a stable backend DOM node id into the (session-
// local) DOM NodeID chromedp's query actions expect.
//
// DOM.describeNode is deliberately NOT used for this: it happily returns a
// node whose NodeID is 0, because a backend node id only gains a frontend
// NodeID once the DOM agent has pushed that node to the frontend. The
// canonical route is to materialise the document first (DOM.getDocument)
// and then ask for the mapping explicitly with
// DOM.pushNodesByBackendIdsToFrontend, which is what an AX-driven flow
// needs since its ids come from the accessibility tree, not from a prior
// DOM query.
func resolveNodeID(ctx context.Context, backendID cdp.BackendNodeID) (cdp.NodeID, error) {
	var ids []cdp.NodeID
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		if _, e := dom.GetDocument().WithDepth(0).Do(actx); e != nil {
			return fmt.Errorf("dom.getDocument: %w", e)
		}
		var e error
		ids, e = dom.PushNodesByBackendIDsToFrontend([]cdp.BackendNodeID{backendID}).Do(actx)
		if e != nil {
			return fmt.Errorf("dom.pushNodesByBackendIdsToFrontend: %w", e)
		}
		return nil
	}))
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 || ids[0] == 0 {
		return 0, fmt.Errorf("no frontend DOM node for backend id %d (node detached or document navigated)", backendID)
	}
	return ids[0], nil
}

// callOnNode resolves backendID to a JS remote object and invokes fn (a
// function declaration whose `this` is the element) on it.
//
// This is deliberately used instead of chromedp's ByNodeID query actions
// (chromedp.Click / chromedp.SetValue): those run chromedp's own
// wait-for-node-ready machinery, which expects the node to have come from a
// chromedp selector query and simply blocks until the caller's context
// expires when handed a node id that came from the accessibility tree
// instead. Calling the DOM method directly is also the more semantic
// action, which is what this backend prefers over synthetic mouse input.
func callOnNode(ctx context.Context, backendID cdp.BackendNodeID, fn string) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		obj, err := dom.ResolveNode().WithBackendNodeID(backendID).Do(actx)
		if err != nil {
			return fmt.Errorf("dom.resolveNode: %w", err)
		}
		if obj == nil || obj.ObjectID == "" {
			return fmt.Errorf("dom.resolveNode returned no remote object for backend id %d", backendID)
		}
		defer func() { _ = runtime.ReleaseObject(obj.ObjectID).Do(actx) }()
		_, exc, err := runtime.CallFunctionOn(fn).
			WithObjectID(obj.ObjectID).
			WithAwaitPromise(false).
			WithReturnByValue(true).
			Do(actx)
		if err != nil {
			return fmt.Errorf("runtime.callFunctionOn: %w", err)
		}
		if exc != nil {
			return fmt.Errorf("runtime.callFunctionOn threw: %s", exc.Text)
		}
		return nil
	}))
}

// Click implements axConn: the element's own DOM click, preferred over
// synthetic mouse input at raw coordinates.
func (c *liveConn) Click(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return err
	}
	defer cancel()
	return callOnNode(ctx, backendID, `function(){ this.click(); }`)
}

// callOnNodeWithArg is callOnNode with a single JSON-serialisable argument
// passed to fn.
func callOnNodeWithArg(ctx context.Context, backendID cdp.BackendNodeID, fn string, arg string) error {
	raw, err := json.Marshal(arg)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		obj, err := dom.ResolveNode().WithBackendNodeID(backendID).Do(actx)
		if err != nil {
			return fmt.Errorf("dom.resolveNode: %w", err)
		}
		if obj == nil || obj.ObjectID == "" {
			return fmt.Errorf("dom.resolveNode returned no remote object for backend id %d", backendID)
		}
		defer func() { _ = runtime.ReleaseObject(obj.ObjectID).Do(actx) }()
		_, exc, err := runtime.CallFunctionOn(fn).
			WithObjectID(obj.ObjectID).
			WithArguments([]*runtime.CallArgument{{Value: raw}}).
			WithAwaitPromise(false).
			WithReturnByValue(true).
			Do(actx)
		if err != nil {
			return fmt.Errorf("runtime.callFunctionOn: %w", err)
		}
		if exc != nil {
			return fmt.Errorf("runtime.callFunctionOn threw: %s", exc.Text)
		}
		return nil
	}))
}

// SetValue implements axConn.
func (c *liveConn) SetValue(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID, text string) error {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return err
	}
	defer cancel()
	// Set the value and fire the input/change events a real edit would,
	// so framework-bound inputs (React/Vue/...) actually observe it.
	js := `function(v){ this.value = v; ` +
		`this.dispatchEvent(new Event("input", {bubbles:true})); ` +
		`this.dispatchEvent(new Event("change", {bubbles:true})); }`
	return callOnNodeWithArg(ctx, backendID, js, text)
}

// InsertText implements axConn: focuses backendID, then inserts text at the
// caret via Input.insertText (works for arbitrary Unicode, unlike a raw key
// event stream).
func (c *liveConn) InsertText(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID, text string) error {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return err
	}
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		return dom.Focus().WithBackendNodeID(backendID).Do(actx)
	})); err != nil {
		return fmt.Errorf("focus before insert text: %w", err)
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(actx context.Context) error {
		return input.InsertText(text).Do(actx)
	}))
}

// ScrollIntoView implements axConn.
func (c *liveConn) ScrollIntoView(callerCtx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error {
	ctx, cancel, err := c.targetContext(callerCtx, targetID)
	if err != nil {
		return err
	}
	defer cancel()
	nodeID, err := resolveNodeID(ctx, backendID)
	if err != nil {
		return err
	}
	return chromedp.Run(ctx, chromedp.ScrollIntoView([]cdp.NodeID{nodeID}, chromedp.ByNodeID))
}

// Close implements axConn: it cancels only the per-target attachments this
// conn created, never the underlying registered browser session (owned by
// internal/llm/tools/browser_session.go).
func (c *liveConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.cancels {
		cancel()
	}
	c.targets = make(map[target.ID]context.Context)
	c.cancels = make(map[target.ID]context.CancelFunc)
	c.enabled = make(map[target.ID]bool)
	return nil
}
