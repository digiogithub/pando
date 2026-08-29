package darwin

import (
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestParseFetchedNodeAndToElement(t *testing.T) {
	ref := axRef{PID: 42, Handle: 0xdeadbeef}
	raw := map[string]any{
		"AXRole":        "AXButton",
		"AXSubrole":     "AXSearchField",
		"AXTitle":       "Search",
		"AXDescription": "Search the document",
		"AXValue":       "query",
		"AXHelp":        "Press to search",
		"AXEnabled":     true,
		"AXFocused":     true,
		"AXSelected":    true,
		"AXIdentifier":  "search-field-1",
		"AXPosition":    Point{X: 10, Y: 20},
		"AXSize":        Size{W: 100, H: 30},
	}
	n := parseFetchedNode(ref, raw)
	if n.role != "AXButton" || n.subrole != "AXSearchField" {
		t.Fatalf("unexpected role/subrole: %+v", n)
	}
	if n.bounds != (core.Bounds{X: 10, Y: 20, W: 100, H: 30}) {
		t.Fatalf("unexpected bounds: %+v", n.bounds)
	}

	el := n.toElement("ax", 42, []int{0, 2})
	if el.Role != core.RoleButton {
		t.Fatalf("expected normalized role button, got %s", el.Role)
	}
	if el.Native.Platform != "ax" || el.Native.Role != "AXButton" || el.Native.SubRole != "AXSearchField" {
		t.Fatalf("Native escape hatch not preserved: %+v", el.Native)
	}
	if el.Native.Data[nativeIdentifierKey] != "search-field-1" {
		t.Fatalf("expected identifier preserved in Native.Data, got %v", el.Native.Data[nativeIdentifierKey])
	}
	if !reflect.DeepEqual(el.Native.Data[nativeIndexPathKey], []int{0, 2}) {
		t.Fatalf("expected index path preserved, got %v", el.Native.Data[nativeIndexPathKey])
	}
	if el.AppID != "42" {
		t.Fatalf("expected AppID '42', got %q", el.AppID)
	}
	if !el.Enabled || !el.Focused {
		t.Fatalf("expected Enabled+Focused true")
	}
}

func TestRefFromElementRoundTrip(t *testing.T) {
	ref := axRef{PID: 7, Handle: 0x1234}
	n := parseFetchedNode(ref, map[string]any{"AXRole": "AXWindow"})
	el := n.toElement("ax", 7, nil)

	got, err := refFromElement(el)
	if err != nil {
		t.Fatalf("refFromElement: %v", err)
	}
	if got != ref {
		t.Fatalf("expected round-trip ref %+v, got %+v", ref, got)
	}
}

func TestRefFromElementMissingHandle(t *testing.T) {
	el := &core.Element{Role: core.RoleWindow}
	_, err := refFromElement(el)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrElementNotFound {
		t.Fatalf("expected ELEMENT_NOT_FOUND, got %v", err)
	}
}

func TestRefFromElementNilElement(t *testing.T) {
	_, err := refFromElement(nil)
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrInvalidArgs {
		t.Fatalf("expected INVALID_ARGS for nil element, got %v", err)
	}
}

func TestActionsForTextFieldGetsSetValue(t *testing.T) {
	n := &fetchedNode{role: "AXTextField"}
	actions := actionsFor(n)
	found := false
	for _, a := range actions {
		if a == core.ActionSetValue {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ActionSetValue for a text field, got %v", actions)
	}
}

func TestFormatAXValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{true, "true"},
		{false, "false"},
		{3.5, "3.5"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := formatAXValue(c.in); got != c.want {
			t.Fatalf("formatAXValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
