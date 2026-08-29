package browser

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/chromedp"
	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/events"
)

// eventMu guards the package-level listener/bus state below. There is
// intentionally one slot, mirroring session.go's single registered
// session: internal/uiauto.Manager (and therefore this backend) is a
// process-wide singleton, so "the" browser session's event stream is
// whichever chromedp session RegisterSession most recently published.
var (
	eventMu      sync.Mutex
	eventBus     *events.EventBus
	eventSession context.Context
)

// ensureEventListener lazily enables the DOM and Accessibility domains and
// installs exactly one chromedp.ListenTarget handler on sessCtx.
// chromedp's ListenTarget has no unsubscribe primitive -- a handler lives
// for its ctx's whole lifetime -- so installing more than one per session
// would leak; every Subscribe call instead shares this one listener via
// eventBus, exactly like the Linux AT-SPI backend shares one D-Bus match
// rule (see internal/uiauto/platform/linux/events.go). Safe to call more
// than once; a session change (a new sessCtx, e.g. after RegisterSession
// republishes) gets its own listener and bus.
func ensureEventListener(sessCtx context.Context) *events.EventBus {
	eventMu.Lock()
	defer eventMu.Unlock()
	if eventBus != nil && eventSession == sessCtx {
		return eventBus
	}

	bus := events.NewEventBus()
	// Enabling twice (across sessions, or if some other tool already
	// enabled these domains) is harmless -- CDP domain Enable calls are
	// idempotent. A failure here just means the listener sees nothing
	// (Subscribe callers still get a valid, if quiet, channel); it never
	// panics or blocks Subscribe. DOM.getDocument is required too: per
	// the CDP DOM domain docs, the client only receives DOM events for
	// nodes it already "knows about" -- without pulling the whole
	// existing subtree once (WithDepth(-1); the default depth of 1
	// leaves every node below the document root "unknown" to the
	// frontend), childNodeInserted/childNodeRemoved/attributeModified
	// never fire for the page's existing tree (confirmed empirically
	// against a live headless Chrome: no depth(-1) pull => zero DOM
	// events for a subsequent JS-driven mutation).
	_ = chromedp.Run(sessCtx,
		dom.Enable(),
		accessibility.Enable(),
		chromedp.ActionFunc(func(actx context.Context) error {
			_, err := dom.GetDocument().WithDepth(-1).Do(actx)
			return err
		}),
	)
	chromedp.ListenTarget(sessCtx, func(ev interface{}) {
		if e, ok := decodeCdpEvent(ev); ok {
			bus.Publish(e)
		}
	})
	eventBus = bus
	eventSession = sessCtx
	return bus
}

// decodeCdpEvent maps a subset of CDP DOM/Accessibility domain events onto
// events.Event. Every other event type chromedp.ListenTarget delivers
// (network, console, lifecycle, ...) is intentionally ignored (ok=false):
// this package only cares about accessibility-tree-relevant changes.
func decodeCdpEvent(ev interface{}) (events.Event, bool) {
	now := time.Now()
	switch e := ev.(type) {
	case *dom.EventChildNodeInserted:
		return events.Event{
			Kind:      events.KindCreated,
			Timestamp: now,
			Details:   map[string]any{"nodeId": nodeIDOf(e.Node), "parentNodeId": e.ParentNodeID},
		}, true
	case *dom.EventChildNodeRemoved:
		return events.Event{
			Kind:      events.KindDestroyed,
			Timestamp: now,
			Details:   map[string]any{"nodeId": e.NodeID, "parentNodeId": e.ParentNodeID},
		}, true
	case *dom.EventAttributeModified:
		return events.Event{
			Kind:      events.KindPropertyChanged,
			Timestamp: now,
			Details:   map[string]any{"nodeId": e.NodeID, "name": e.Name, "value": e.Value},
		}, true
	case *accessibility.EventNodesUpdated:
		return events.Event{
			Kind:      events.KindPropertyChanged,
			Timestamp: now,
			Details:   map[string]any{"nodesUpdated": len(e.Nodes)},
		}, true
	default:
		return events.Event{}, false
	}
}

func nodeIDOf(n *cdp.Node) interface{} {
	if n == nil {
		return nil
	}
	return n.NodeID
}

// Subscribe implements events.Subscriber for CdpBackend. scope is
// currently not used to filter server-side (CDP's DOM/Accessibility
// domain events are not natively scopable to an arbitrary selector);
// events.WaitFor always re-evaluates the actual locator/condition against
// the backend on every received event, so an unrelated event only costs
// one harmless extra Find call, never an incorrect result.
func (b *CdpBackend) Subscribe(ctx context.Context, scope core.Scope) (<-chan events.Event, func(), error) {
	sessCtx, ok := ActiveSession()
	if !ok {
		return nil, nil, errNoActiveSession
	}
	bus := ensureEventListener(sessCtx)
	ch, unsub := bus.Subscribe(32)
	return ch, unsub, nil
}
