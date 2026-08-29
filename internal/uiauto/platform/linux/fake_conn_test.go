package linux

import (
	"context"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// fakeNode is one node of an in-memory, D-Bus-shaped AT-SPI tree used by
// tests to exercise the traversal/matching logic without a real
// accessibility bus.
type fakeNode struct {
	ref         accessibleRef
	name        string
	description string
	roleName    string
	state       []uint32
	interfaces  []string
	children    []accessibleRef
	bounds      [4]int32 // x, y, w, h
	value       float64
	actionNames []string
	// invoked/text records calls this test made against the node, so tests
	// can assert on side effects.
	invoked    []string
	text       string
	focused    bool // updated by GrabFocus
	editedText string
}

// fakeConn is a busConn backed by an in-memory map of fakeNode, keyed by
// accessibleRef. It implements just enough of the AT-SPI wire protocol
// (Accessible/Component/Action/EditableText/Value/Text methods and
// properties) for the traversal and action code under test.
type fakeConn struct {
	nodes map[accessibleRef]*fakeNode
	// failRefs, when non-nil, makes getAllProps/call return an error for
	// the listed refs, to test error-tolerant traversal.
	failRefs map[accessibleRef]bool
	// callCount tracks how many times each (ref, method) pair was invoked,
	// so tests can assert the per-call memo cache avoids redundant fetches.
	callCount map[string]int
}

func newFakeConn() *fakeConn {
	return &fakeConn{nodes: make(map[accessibleRef]*fakeNode), callCount: make(map[string]int)}
}

func (f *fakeConn) count(r accessibleRef, method string) {
	f.callCount[r.String()+":"+method]++
}

func (f *fakeConn) add(n *fakeNode) *fakeNode {
	f.nodes[n.ref] = n
	return n
}

func ref(bus, path string) accessibleRef {
	return accessibleRef{Bus: bus, Path: dbus.ObjectPath(path)}
}

func (f *fakeConn) getAllProps(ctx context.Context, dest string, path dbus.ObjectPath, iface string) (map[string]dbus.Variant, error) {
	r := accessibleRef{Bus: dest, Path: path}
	f.count(r, "GetAll:"+iface)
	if f.failRefs[r] {
		return nil, fmt.Errorf("fake: forced failure for %s", r)
	}
	n, ok := f.nodes[r]
	if !ok {
		return nil, fmt.Errorf("fake: unknown object %s", r)
	}
	switch iface {
	case accessibleIface:
		return map[string]dbus.Variant{
			"Name":        dbus.MakeVariant(n.name),
			"Description": dbus.MakeVariant(n.description),
			"ChildCount":  dbus.MakeVariant(int32(len(n.children))),
		}, nil
	case valueIface:
		return map[string]dbus.Variant{"CurrentValue": dbus.MakeVariant(n.value)}, nil
	case actionIface:
		return map[string]dbus.Variant{"NActions": dbus.MakeVariant(int32(len(n.actionNames)))}, nil
	default:
		return map[string]dbus.Variant{}, nil
	}
}

func (f *fakeConn) call(ctx context.Context, dest string, path dbus.ObjectPath, iface, method string, args ...interface{}) ([]interface{}, error) {
	r := accessibleRef{Bus: dest, Path: path}
	f.count(r, iface+"."+method)
	if f.failRefs[r] {
		return nil, fmt.Errorf("fake: forced failure for %s", r)
	}
	n, ok := f.nodes[r]
	if !ok {
		return nil, fmt.Errorf("fake: unknown object %s", r)
	}
	if iface != accessibleIface {
		short := iface[strings.LastIndex(iface, ".")+1:]
		if !hasIface(n.interfaces, short) {
			return nil, fmt.Errorf("fake: %s does not implement %s (org.freedesktop.DBus.Error.UnknownMethod)", r, iface)
		}
	}
	switch iface + "." + method {
	case accessibleIface + ".GetRoleName":
		return []interface{}{n.roleName}, nil
	case accessibleIface + ".GetState":
		return []interface{}{append([]uint32(nil), n.state...)}, nil
	case accessibleIface + ".GetInterfaces":
		return []interface{}{append([]string(nil), n.interfaces...)}, nil
	case accessibleIface + ".GetChildren":
		out := make([]interface{}, 0, len(n.children))
		for _, c := range n.children {
			out = append(out, []interface{}{c.Bus, c.Path})
		}
		return []interface{}{out}, nil
	case componentIface + ".GetExtents":
		return []interface{}{n.bounds[0], n.bounds[1], n.bounds[2], n.bounds[3]}, nil
	case componentIface + ".GrabFocus":
		n.focused = true
		return []interface{}{true}, nil
	case actionIface + ".GetNActions":
		return []interface{}{int32(len(n.actionNames))}, nil
	case actionIface + ".GetName":
		idx := args[0].(int32)
		if int(idx) < 0 || int(idx) >= len(n.actionNames) {
			return []interface{}{""}, nil
		}
		return []interface{}{n.actionNames[idx]}, nil
	case actionIface + ".DoAction":
		idx := args[0].(int32)
		if int(idx) < 0 || int(idx) >= len(n.actionNames) {
			return []interface{}{false}, nil
		}
		n.invoked = append(n.invoked, n.actionNames[idx])
		return []interface{}{true}, nil
	case editTextIface + ".SetTextContents":
		n.editedText = args[0].(string)
		return []interface{}{true}, nil
	case textIface + ".GetText":
		return []interface{}{n.text}, nil
	default:
		return nil, fmt.Errorf("fake: unhandled method %s.%s", iface, method)
	}
}

func (f *fakeConn) close() error { return nil }
