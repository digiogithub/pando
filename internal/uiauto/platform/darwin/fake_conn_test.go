package darwin

import (
	"context"
	"fmt"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// fakeNode is one synthetic AXUIElement in an in-memory tree used to test
// the platform-independent traversal/action/backend logic without a real
// Accessibility bus.
type fakeNode struct {
	ref         axRef
	attrs       map[string]any
	actionNames []string
}

// fakeAXConn implements axConn against an in-memory map, with per-ref call
// counters so tests can assert the memoized traverseState never re-fetches
// the same node twice within one Find/Children call.
type fakeAXConn struct {
	nodes      map[axRef]*fakeNode
	apps       []appProc
	appElems   map[int32]axRef
	trustedVal bool
	attrCalls  map[axRef]int
	performed  []string // "pid:handle:action"
	setAttrs   map[axRef]map[string]any
	closed     bool
}

func newFakeAXConn() *fakeAXConn {
	return &fakeAXConn{
		nodes:     make(map[axRef]*fakeNode),
		appElems:  make(map[int32]axRef),
		attrCalls: make(map[axRef]int),
		setAttrs:  make(map[axRef]map[string]any),
	}
}

func (c *fakeAXConn) addNode(ref axRef, attrs map[string]any, actions ...string) *fakeNode {
	n := &fakeNode{ref: ref, attrs: attrs, actionNames: actions}
	c.nodes[ref] = n
	return n
}

func nodeRef(pid int32, handle uintptr) axRef { return axRef{PID: pid, Handle: handle} }

func (c *fakeAXConn) attributes(ctx context.Context, ref axRef, names []string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.attrCalls[ref]++
	n, ok := c.nodes[ref]
	if !ok {
		return nil, core.NewStaleRefError(fmt.Sprintf("fake: no such element %s", ref))
	}
	out := make(map[string]any, len(names))
	for _, name := range names {
		if v, ok := n.attrs[name]; ok {
			out[name] = v
		}
	}
	return out, nil
}

func (c *fakeAXConn) actionNames(ctx context.Context, ref axRef) ([]string, error) {
	n, ok := c.nodes[ref]
	if !ok {
		return nil, core.NewStaleRefError("fake: no such element")
	}
	return n.actionNames, nil
}

func (c *fakeAXConn) performAction(ctx context.Context, ref axRef, name string) error {
	if _, ok := c.nodes[ref]; !ok {
		return core.NewStaleRefError("fake: no such element")
	}
	c.performed = append(c.performed, fmt.Sprintf("%d:%x:%s", ref.PID, ref.Handle, name))
	return nil
}

func (c *fakeAXConn) setAttribute(ctx context.Context, ref axRef, attr string, value any) error {
	n, ok := c.nodes[ref]
	if !ok {
		return core.NewStaleRefError("fake: no such element")
	}
	if c.setAttrs[ref] == nil {
		c.setAttrs[ref] = make(map[string]any)
	}
	c.setAttrs[ref][attr] = value
	n.attrs[attr] = value
	return nil
}

func (c *fakeAXConn) runningApps(ctx context.Context) ([]appProc, error) {
	return c.apps, nil
}

func (c *fakeAXConn) appElement(ctx context.Context, pid int32) (axRef, error) {
	ref, ok := c.appElems[pid]
	if !ok {
		return axRef{}, core.NewAppNotFoundError("fake: no such app")
	}
	return ref, nil
}

func (c *fakeAXConn) trusted(ctx context.Context) bool { return c.trustedVal }

func (c *fakeAXConn) close() error {
	c.closed = true
	return nil
}
