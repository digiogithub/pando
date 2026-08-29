package linux

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/godbus/dbus/v5"
)

func TestFetchNodeAndToElement(t *testing.T) {
	conn := newFakeConn()
	r := ref("app1", "/save")
	conn.add(&fakeNode{
		ref: r, roleName: "push button", name: "Save", description: "Save the document",
		interfaces: []string{"org.a11y.atspi.Action", "org.a11y.atspi.Component"},
		state:      stateWord(stateEnabled, stateSensitive, stateVisible, stateShowing, stateFocused),
		bounds:     [4]int32{10, 20, 100, 30},
	})

	n, err := fetchNode(context.Background(), conn, r)
	if err != nil {
		t.Fatalf("fetchNode failed: %v", err)
	}
	el := n.toElement("atspi", "app1")

	if el.Role != core.RoleButton {
		t.Fatalf("expected role button, got %s", el.Role)
	}
	if el.Name != "Save" || el.Description != "Save the document" {
		t.Fatalf("unexpected name/description: %+v", el)
	}
	if !el.Enabled || !el.Visible || !el.Focused {
		t.Fatalf("expected Enabled/Visible/Focused all true, got %+v", el)
	}
	if el.Bounds != (core.Bounds{X: 10, Y: 20, W: 100, H: 30}) {
		t.Fatalf("unexpected bounds: %+v", el.Bounds)
	}
	if el.Native.Platform != "atspi" || el.Native.Role != "push button" {
		t.Fatalf("unexpected native data: %+v", el.Native)
	}
	if el.Native.Data[nativeBusKey] != "app1" || el.Native.Data[nativePathKey] != "/save" {
		t.Fatalf("expected native handle to carry busName/objectPath, got %+v", el.Native.Data)
	}
}

func TestRefFromElementPrefersNativeHandle(t *testing.T) {
	el := &core.Element{
		AppID:    "wrong-bus",
		WindowID: "/wrong-path",
		Native: core.NativeData{Data: map[string]any{
			nativeBusKey:  "app1",
			nativePathKey: "/real-path",
		}},
	}
	got, err := refFromElement(el)
	if err != nil {
		t.Fatalf("refFromElement failed: %v", err)
	}
	want := accessibleRef{Bus: "app1", Path: dbus.ObjectPath("/real-path")}
	if got != want {
		t.Fatalf("expected native handle to win, got %+v want %+v", got, want)
	}
}

func TestRefFromElementFallsBackToAppWindowID(t *testing.T) {
	// The synthetic root Element Manager.rootElement builds from WindowInfo
	// carries no Native data at all.
	el := &core.Element{AppID: "app1", WindowID: "/win1"}
	got, err := refFromElement(el)
	if err != nil {
		t.Fatalf("refFromElement failed: %v", err)
	}
	want := accessibleRef{Bus: "app1", Path: dbus.ObjectPath("/win1")}
	if got != want {
		t.Fatalf("expected fallback ref, got %+v want %+v", got, want)
	}
}

func TestRefFromElementErrorsWithoutAnyHandle(t *testing.T) {
	_, err := refFromElement(&core.Element{})
	de, ok := core.AsDesktopError(err)
	if !ok || de.Code != core.ErrElementNotFound {
		t.Fatalf("expected ELEMENT_NOT_FOUND for a handle-less element, got %v", err)
	}
}

func TestActionsForHeuristics(t *testing.T) {
	n := &fetchedNode{interfaces: []string{"org.a11y.atspi.Action", "org.a11y.atspi.EditableText"}}
	ds := decodedState{Checkable: true, Selectable: true, Expandable: true}
	actions := actionsFor(n, ds)

	want := map[core.ActionKind]bool{
		core.ActionInvoke: true, core.ActionSetValue: true, core.ActionType: true,
		core.ActionToggle: true, core.ActionSelect: true, core.ActionExpand: true, core.ActionCollapse: true,
	}
	got := map[core.ActionKind]bool{}
	for _, a := range actions {
		got[a] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("expected action %s to be present, got %v", k, actions)
		}
	}
}
