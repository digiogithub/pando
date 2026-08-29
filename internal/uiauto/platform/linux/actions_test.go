package linux

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestPerformInvokePicksClickLikeAction(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/save")
	conn.add(&fakeNode{ref: r, roleName: "push button", name: "Save",
		interfaces:  []string{"org.a11y.atspi.Action"},
		actionNames: []string{"tooltip", "click", "context-menu"}})

	if err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionInvoke}); err != nil {
		t.Fatalf("performAction(invoke) failed: %v", err)
	}
	n := conn.nodes[r]
	if len(n.invoked) != 1 || n.invoked[0] != "click" {
		t.Fatalf("expected DoAction to pick the 'click' action, invoked=%v", n.invoked)
	}
}

func TestPerformInvokeFallsBackToIndexZero(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/widget")
	conn.add(&fakeNode{ref: r, roleName: "push button", name: "Widget",
		interfaces:  []string{"org.a11y.atspi.Action"},
		actionNames: []string{"do-the-thing"}})

	if err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionInvoke}); err != nil {
		t.Fatalf("performAction(invoke) failed: %v", err)
	}
	n := conn.nodes[r]
	if len(n.invoked) != 1 || n.invoked[0] != "do-the-thing" {
		t.Fatalf("expected fallback to index 0, invoked=%v", n.invoked)
	}
}

func TestPerformInvokeWithoutActionInterfaceFails(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/label")
	conn.add(&fakeNode{ref: r, roleName: "label", name: "Just text"})

	err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionInvoke})
	if err == nil {
		t.Fatalf("expected an error for an element with no Action interface")
	}
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("expected ACTION_FAILED, got %v", err)
	}
}

func TestPerformFocusCallsGrabFocus(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/field")
	conn.add(&fakeNode{ref: r, roleName: "entry", name: "Field", interfaces: []string{"org.a11y.atspi.Component"}})

	if err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionFocus}); err != nil {
		t.Fatalf("performAction(focus) failed: %v", err)
	}
	if !conn.nodes[r].focused {
		t.Fatalf("expected GrabFocus to be invoked")
	}
}

func TestPerformSetValueUsesEditableText(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/field")
	conn.add(&fakeNode{ref: r, roleName: "entry", name: "Field", interfaces: []string{"org.a11y.atspi.EditableText"}})

	if err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionSetValue, Text: "hello"}); err != nil {
		t.Fatalf("performAction(setvalue) failed: %v", err)
	}
	if conn.nodes[r].editedText != "hello" {
		t.Fatalf("expected SetTextContents(\"hello\"), got %q", conn.nodes[r].editedText)
	}
}

func TestPerformSetValueWithoutEditableTextFails(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/label")
	conn.add(&fakeNode{ref: r, roleName: "label", name: "Just text"})

	err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionType, Text: "x"})
	if err == nil {
		t.Fatalf("expected an error when EditableText is not implemented")
	}
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("expected ACTION_FAILED, got %v", err)
	}
}

func TestPerformExpandCollapseUseNamedActions(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/tree-item")
	conn.add(&fakeNode{ref: r, roleName: "tree item", name: "Node",
		interfaces:  []string{"org.a11y.atspi.Action"},
		actionNames: []string{"expand", "collapse"}})

	if err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionExpand}); err != nil {
		t.Fatalf("performAction(expand) failed: %v", err)
	}
	if err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionCollapse}); err != nil {
		t.Fatalf("performAction(collapse) failed: %v", err)
	}
	n := conn.nodes[r]
	if len(n.invoked) != 2 || n.invoked[0] != "expand" || n.invoked[1] != "collapse" {
		t.Fatalf("expected expand then collapse, invoked=%v", n.invoked)
	}
}

func TestPerformUnknownActionKindIsPlatformNotSupported(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/x")
	conn.add(&fakeNode{ref: r, roleName: "button", name: "X"})

	err := performAction(context.Background(), conn, r, core.Action{Kind: core.ActionScroll})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED for an unmapped action kind, got %v", err)
	}
}
