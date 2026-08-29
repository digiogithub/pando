package darwin

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestPerformActionInvokePicksPress(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXButton"}, "AXCancel", "AXPress")
	if err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionInvoke}); err != nil {
		t.Fatalf("performAction invoke: %v", err)
	}
	if len(conn.performed) != 1 || conn.performed[0] != "1:a:AXPress" {
		t.Fatalf("expected AXPress to be performed, got %v", conn.performed)
	}
}

func TestPerformActionInvokeFallsBackToFirstAction(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXButton"}, "AXCustomThing")
	if err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionInvoke}); err != nil {
		t.Fatalf("performAction invoke: %v", err)
	}
	if len(conn.performed) != 1 || conn.performed[0] != "1:a:AXCustomThing" {
		t.Fatalf("expected fallback to first advertised action, got %v", conn.performed)
	}
}

func TestPerformActionInvokeFailsWithoutActions(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXButton"})
	err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionInvoke})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrActionFailed {
		t.Fatalf("expected ACTION_FAILED, got %v", err)
	}
}

func TestPerformActionFocusSetsAttribute(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXTextField"})
	if err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionFocus}); err != nil {
		t.Fatalf("performAction focus: %v", err)
	}
	if v, _ := conn.setAttrs[ref]["AXFocused"].(bool); !v {
		t.Fatalf("expected AXFocused=true to be set")
	}
}

func TestPerformActionSetValue(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXTextField"})
	if err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionSetValue, Text: "hello"}); err != nil {
		t.Fatalf("performAction setvalue: %v", err)
	}
	if v, _ := conn.setAttrs[ref]["AXValue"].(string); v != "hello" {
		t.Fatalf("expected AXValue=hello, got %v", conn.setAttrs[ref]["AXValue"])
	}
}

func TestPerformActionExpandCollapse(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXDisclosureTriangle"}, "AXShowMenu", "AXCancel")
	if err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionExpand}); err != nil {
		t.Fatalf("performAction expand: %v", err)
	}
	if err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionCollapse}); err != nil {
		t.Fatalf("performAction collapse: %v", err)
	}
	if len(conn.performed) != 2 || conn.performed[0] != "1:a:AXShowMenu" || conn.performed[1] != "1:a:AXCancel" {
		t.Fatalf("expected AXShowMenu then AXCancel, got %v", conn.performed)
	}
}

func TestPerformActionUnmappedKindReturnsPlatformNotSupported(t *testing.T) {
	conn := newFakeAXConn()
	ref := nodeRef(1, 10)
	conn.addNode(ref, map[string]any{"AXRole": "AXButton"})
	err := performAction(context.Background(), conn, ref, core.Action{Kind: core.ActionScroll})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrPlatformNotSupported {
		t.Fatalf("expected PLATFORM_NOT_SUPPORTED for scroll, got %v", err)
	}
}
