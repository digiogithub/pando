package browser

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/events"
)

func TestDecodeCdpEventChildNodeInserted(t *testing.T) {
	ev := &dom.EventChildNodeInserted{
		ParentNodeID: cdp.NodeID(1),
		Node:         &cdp.Node{NodeID: cdp.NodeID(2)},
	}
	e, ok := decodeCdpEvent(ev)
	if !ok || e.Kind != events.KindCreated {
		t.Fatalf("expected KindCreated, got ok=%v kind=%q", ok, e.Kind)
	}
	if e.Details["nodeId"] != cdp.NodeID(2) {
		t.Fatalf("Details[nodeId] = %v", e.Details["nodeId"])
	}
}

func TestDecodeCdpEventChildNodeInsertedNilNode(t *testing.T) {
	ev := &dom.EventChildNodeInserted{ParentNodeID: cdp.NodeID(1)}
	e, ok := decodeCdpEvent(ev)
	if !ok || e.Kind != events.KindCreated {
		t.Fatalf("expected KindCreated even with a nil Node, got ok=%v kind=%q", ok, e.Kind)
	}
	if e.Details["nodeId"] != nil {
		t.Fatalf("expected nil nodeId for a nil Node, got %v", e.Details["nodeId"])
	}
}

func TestDecodeCdpEventChildNodeRemoved(t *testing.T) {
	ev := &dom.EventChildNodeRemoved{ParentNodeID: cdp.NodeID(1), NodeID: cdp.NodeID(3)}
	e, ok := decodeCdpEvent(ev)
	if !ok || e.Kind != events.KindDestroyed {
		t.Fatalf("expected KindDestroyed, got ok=%v kind=%q", ok, e.Kind)
	}
}

func TestDecodeCdpEventAttributeModified(t *testing.T) {
	ev := &dom.EventAttributeModified{NodeID: cdp.NodeID(4), Name: "value", Value: "hi"}
	e, ok := decodeCdpEvent(ev)
	if !ok || e.Kind != events.KindPropertyChanged {
		t.Fatalf("expected KindPropertyChanged, got ok=%v kind=%q", ok, e.Kind)
	}
	if e.Details["name"] != "value" || e.Details["value"] != "hi" {
		t.Fatalf("unexpected Details: %+v", e.Details)
	}
}

func TestDecodeCdpEventNodesUpdated(t *testing.T) {
	ev := &accessibility.EventNodesUpdated{Nodes: []*accessibility.Node{{}, {}}}
	e, ok := decodeCdpEvent(ev)
	if !ok || e.Kind != events.KindPropertyChanged {
		t.Fatalf("expected KindPropertyChanged, got ok=%v kind=%q", ok, e.Kind)
	}
	if e.Details["nodesUpdated"] != 2 {
		t.Fatalf("Details[nodesUpdated] = %v, want 2", e.Details["nodesUpdated"])
	}
}

func TestDecodeCdpEventIgnoresUnrelatedEventTypes(t *testing.T) {
	if _, ok := decodeCdpEvent("not an event"); ok {
		t.Fatal("expected an unrecognized event value to be ignored")
	}
	if _, ok := decodeCdpEvent(nil); ok {
		t.Fatal("expected nil to be ignored")
	}
}

func TestCdpBackendSubscribeNoActiveSession(t *testing.T) {
	sessionMu.Lock()
	sessionID, sessionCtx = "", nil
	sessionMu.Unlock()

	b := &CdpBackend{conn: newLiveConn()}
	_, _, err := b.Subscribe(context.Background(), core.Scope{})
	if err == nil {
		t.Fatal("expected an error when no session is registered")
	}
}
