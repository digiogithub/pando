package browser

import (
	"context"
	"errors"
	"sync"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/target"
)

// fakeConn is an in-memory axConn implementation used to unit-test role
// mapping, AX-node -> Element conversion, selector-driven traversal/
// pruning, action dispatch and error mapping without a real browser.
type fakeConn struct {
	mu sync.Mutex

	version string
	targets []*target.Info

	// nodes indexes every node by (targetID, NodeID).
	nodes map[target.ID]map[accessibility.NodeID]*accessibility.Node
	// children indexes a node's direct children by (targetID, NodeID).
	children map[target.ID]map[accessibility.NodeID][]*accessibility.Node
	// byBackend indexes a node by (targetID, BackendDOMNodeID), for
	// PartialTree/BoxModel/action calls that only carry a backend id.
	byBackend map[target.ID]map[cdp.BackendNodeID]*accessibility.Node

	boxModels map[cdp.BackendNodeID]*dom.BoxModel

	// failVersion / failTargets / failRootNode force an error from the
	// corresponding method, for Available()/error-path tests.
	failVersion  error
	failTargets  error
	failRootNode map[target.ID]error
	failChildren map[target.ID]map[accessibility.NodeID]error
	failAction   map[cdp.BackendNodeID]error

	// Call recordings, for action-dispatch assertions.
	focused        []cdp.BackendNodeID
	clicked        []cdp.BackendNodeID
	scrolled       []cdp.BackendNodeID
	setValues      map[cdp.BackendNodeID]string
	insertedText   map[cdp.BackendNodeID]string
	childNodeCalls int
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		nodes:        make(map[target.ID]map[accessibility.NodeID]*accessibility.Node),
		children:     make(map[target.ID]map[accessibility.NodeID][]*accessibility.Node),
		byBackend:    make(map[target.ID]map[cdp.BackendNodeID]*accessibility.Node),
		boxModels:    make(map[cdp.BackendNodeID]*dom.BoxModel),
		failRootNode: make(map[target.ID]error),
		failChildren: make(map[target.ID]map[accessibility.NodeID]error),
		failAction:   make(map[cdp.BackendNodeID]error),
		setValues:    make(map[cdp.BackendNodeID]string),
		insertedText: make(map[cdp.BackendNodeID]string),
		version:      "Chrome/999.0.0.0",
	}
}

// addNode registers node under targetID and, when parent != "", wires it as
// a child of parent (also under targetID). It indexes the node by both its
// AX NodeID and its BackendDOMNodeID (when non-zero).
func (f *fakeConn) addNode(targetID target.ID, node *accessibility.Node, parent accessibility.NodeID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodes[targetID] == nil {
		f.nodes[targetID] = make(map[accessibility.NodeID]*accessibility.Node)
	}
	f.nodes[targetID][node.NodeID] = node
	if node.BackendDOMNodeID != 0 {
		if f.byBackend[targetID] == nil {
			f.byBackend[targetID] = make(map[cdp.BackendNodeID]*accessibility.Node)
		}
		f.byBackend[targetID][node.BackendDOMNodeID] = node
	}
	if parent != "" {
		if f.children[targetID] == nil {
			f.children[targetID] = make(map[accessibility.NodeID][]*accessibility.Node)
		}
		f.children[targetID][parent] = append(f.children[targetID][parent], node)
	}
}

func (f *fakeConn) setBoxModel(backendID cdp.BackendNodeID, model *dom.BoxModel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boxModels[backendID] = model
}

// targetIDFor is a tiny readability helper for tests: target.ID is just a
// string type.
func targetIDFor(s string) target.ID { return target.ID(s) }

func axVal(s string) *accessibility.Value {
	return &accessibility.Value{Type: accessibility.ValueTypeString, Value: []byte(`"` + s + `"`)}
}

func axBoolVal(b bool) *accessibility.Value {
	v := "false"
	if b {
		v = "true"
	}
	return &accessibility.Value{Type: accessibility.ValueTypeBoolean, Value: []byte(v)}
}

func (f *fakeConn) Version(ctx context.Context) (string, error) {
	if f.failVersion != nil {
		return "", f.failVersion
	}
	return f.version, nil
}

func (f *fakeConn) Targets(ctx context.Context) ([]*target.Info, error) {
	if f.failTargets != nil {
		return nil, f.failTargets
	}
	return f.targets, nil
}

func (f *fakeConn) RootNode(ctx context.Context, targetID target.ID) (*accessibility.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failRootNode[targetID]; err != nil {
		return nil, err
	}
	for _, n := range f.nodes[targetID] {
		// The root is the node no other node lists as a child.
		isChild := false
		for _, kids := range f.children[targetID] {
			for _, k := range kids {
				if k.NodeID == n.NodeID {
					isChild = true
				}
			}
		}
		if !isChild {
			return n, nil
		}
	}
	return nil, errors.New("fakeConn: no root node registered for target " + string(targetID))
}

func (f *fakeConn) ChildNodes(ctx context.Context, targetID target.ID, nodeID accessibility.NodeID) ([]*accessibility.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.childNodeCalls++
	if perNode, ok := f.failChildren[targetID]; ok {
		if err := perNode[nodeID]; err != nil {
			return nil, err
		}
	}
	return f.children[targetID][nodeID], nil
}

func (f *fakeConn) PartialTree(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) ([]*accessibility.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.byBackend[targetID][backendID]; ok {
		return []*accessibility.Node{n}, nil
	}
	return nil, errors.New("fakeConn: no node for backend id")
}

func (f *fakeConn) BoxModel(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) (*dom.BoxModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boxModels[backendID], nil
}

func (f *fakeConn) Focus(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failAction[backendID]; err != nil {
		return err
	}
	f.focused = append(f.focused, backendID)
	return nil
}

func (f *fakeConn) Click(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failAction[backendID]; err != nil {
		return err
	}
	f.clicked = append(f.clicked, backendID)
	return nil
}

func (f *fakeConn) SetValue(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failAction[backendID]; err != nil {
		return err
	}
	f.setValues[backendID] = text
	return nil
}

func (f *fakeConn) InsertText(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failAction[backendID]; err != nil {
		return err
	}
	f.insertedText[backendID] = text
	return nil
}

func (f *fakeConn) ScrollIntoView(ctx context.Context, targetID target.ID, backendID cdp.BackendNodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failAction[backendID]; err != nil {
		return err
	}
	f.scrolled = append(f.scrolled, backendID)
	return nil
}

func (f *fakeConn) Close() error { return nil }

var _ axConn = (*fakeConn)(nil)
