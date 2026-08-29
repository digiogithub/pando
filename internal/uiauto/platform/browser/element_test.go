package browser

import (
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestToElementBasicMapping(t *testing.T) {
	n := &accessibility.Node{
		NodeID:           "7",
		BackendDOMNodeID: cdp.BackendNodeID(42),
		Role:             axVal("button"),
		Name:             axVal("Save"),
		Value:            axVal("save-value"),
		Description:      axVal("Saves the document"),
		Properties: []*accessibility.Property{
			{Name: accessibility.PropertyNameFocusable, Value: axBoolVal(true)},
			{Name: accessibility.PropertyNameFocused, Value: axBoolVal(true)},
			{Name: accessibility.PropertyNameDisabled, Value: axBoolVal(false)},
		},
	}

	el := toElement(n, "T1")

	if el.Role != core.RoleButton {
		t.Fatalf("role = %q, want button", el.Role)
	}
	if el.Name != "Save" {
		t.Fatalf("name = %q", el.Name)
	}
	if el.Value != "save-value" {
		t.Fatalf("value = %q", el.Value)
	}
	if el.Description != "Saves the document" {
		t.Fatalf("description = %q", el.Description)
	}
	if !el.Enabled {
		t.Fatal("expected Enabled=true")
	}
	if !el.Visible {
		t.Fatal("expected Visible=true (Ignored=false, no hidden prop)")
	}
	if !el.Focused {
		t.Fatal("expected Focused=true")
	}
	if el.Backend != "cdp" {
		t.Fatalf("backend = %q", el.Backend)
	}
	if el.AppID != appID {
		t.Fatalf("appID = %q, want %q", el.AppID, appID)
	}
	if el.Native.Platform != "cdp" || el.Native.Role != "button" {
		t.Fatalf("native = %+v", el.Native)
	}
	if got := el.Native.Data[nativeTargetIDKey]; got != "T1" {
		t.Fatalf("native targetId = %v", got)
	}
	if got := el.Native.Data[nativeAXNodeIDKey]; got != "7" {
		t.Fatalf("native axNodeId = %v", got)
	}
	if got, ok := el.Native.Data[nativeBackendIDKey].(int64); !ok || got != 42 {
		t.Fatalf("native backendDOMNodeId = %v", el.Native.Data[nativeBackendIDKey])
	}

	hasFocus, hasInvoke := false, false
	for _, a := range el.Actions {
		if a == core.ActionFocus {
			hasFocus = true
		}
		if a == core.ActionInvoke {
			hasInvoke = true
		}
	}
	if !hasFocus || !hasInvoke {
		t.Fatalf("actions = %v, want focus+invoke for a focusable button", el.Actions)
	}
}

func TestToElementDisabledHiddenIgnored(t *testing.T) {
	n := &accessibility.Node{
		NodeID:  "1",
		Ignored: true,
		Role:    axVal("generic"),
		Properties: []*accessibility.Property{
			{Name: accessibility.PropertyNameDisabled, Value: axBoolVal(true)},
			{Name: accessibility.PropertyNameHidden, Value: axBoolVal(true)},
		},
	}
	el := toElement(n, "T1")
	if el.Enabled {
		t.Fatal("expected Enabled=false")
	}
	if el.Visible {
		t.Fatal("expected Visible=false (Ignored + hidden)")
	}
	if v, ok := el.Native.Data["ignored"]; !ok || v != true {
		t.Fatalf("expected native ignored=true, got %v", el.Native.Data["ignored"])
	}
}

func TestToElementEditableRoleActions(t *testing.T) {
	n := &accessibility.Node{
		NodeID: "3",
		Role:   axVal("textField"),
	}
	el := toElement(n, "T1")
	if el.Role != core.RoleTextField {
		t.Fatalf("role = %q", el.Role)
	}
	wantSet, wantType := false, false
	for _, a := range el.Actions {
		if a == core.ActionSetValue {
			wantSet = true
		}
		if a == core.ActionType {
			wantType = true
		}
	}
	if !wantSet || !wantType {
		t.Fatalf("actions = %v, want setvalue+type for a textField", el.Actions)
	}
}

func TestToElementCheckedExpandedExtras(t *testing.T) {
	n := &accessibility.Node{
		NodeID: "5",
		Role:   axVal("checkbox"),
		Properties: []*accessibility.Property{
			{Name: accessibility.PropertyNameChecked, Value: axVal("true")},
			{Name: accessibility.PropertyNameExpanded, Value: axBoolVal(false)},
		},
	}
	el := toElement(n, "T1")
	wantToggle, wantExpand, wantCollapse := false, false, false
	for _, a := range el.Actions {
		switch a {
		case core.ActionToggle:
			wantToggle = true
		case core.ActionExpand:
			wantExpand = true
		case core.ActionCollapse:
			wantCollapse = true
		}
	}
	if !wantToggle || !wantExpand || !wantCollapse {
		t.Fatalf("actions = %v, want toggle+expand+collapse", el.Actions)
	}
	if _, ok := el.Native.Data[string(accessibility.PropertyNameChecked)]; !ok {
		t.Fatalf("expected 'checked' stashed into Native.Data, got %+v", el.Native.Data)
	}
}

func TestRefFromElementNativeHandle(t *testing.T) {
	el := &core.Element{
		Native: core.NativeData{
			Data: map[string]any{
				nativeTargetIDKey:  "T1",
				nativeAXNodeIDKey:  "9",
				nativeBackendIDKey: int64(11),
			},
		},
	}
	targetID, axNodeID, hasAX, err := refFromElement(el)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(targetID) != "T1" || string(axNodeID) != "9" || !hasAX {
		t.Fatalf("got targetID=%q axNodeID=%q hasAX=%v", targetID, axNodeID, hasAX)
	}
	backendID, ok := backendNodeIDFromElement(el)
	if !ok || backendID != 11 {
		t.Fatalf("backendID = %v, ok=%v", backendID, ok)
	}
}

func TestRefFromElementSyntheticRootFallback(t *testing.T) {
	// Mirrors the Manager's synthetic root Element built from a WindowInfo
	// (no Native at all): only WindowID is set.
	el := &core.Element{WindowID: "T2"}
	targetID, _, hasAX, err := refFromElement(el)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(targetID) != "T2" {
		t.Fatalf("targetID = %q", targetID)
	}
	if hasAX {
		t.Fatal("expected hasAX=false for a synthetic root with no Native handle")
	}
}

func TestRefFromElementNoHandleAtAll(t *testing.T) {
	el := &core.Element{}
	if _, _, _, err := refFromElement(el); err == nil {
		t.Fatal("expected an error for an element with no target/window identity")
	}
}

func TestRefFromElementNil(t *testing.T) {
	if _, _, _, err := refFromElement(nil); err == nil {
		t.Fatal("expected an error for a nil element")
	}
}
