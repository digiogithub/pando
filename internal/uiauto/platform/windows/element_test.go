package windows

import (
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestBuildElement(t *testing.T) {
	p := cachedProps{
		RuntimeID:     []int32{42, 7},
		Name:          "OK",
		AutomationID:  "okBtn",
		ClassName:     "Button",
		ControlType:   ControlTypeButton,
		Bounds:        core.Bounds{X: 1, Y: 2, W: 3, H: 4},
		Enabled:       true,
		Offscreen:     false,
		KeyboardFocus: true,
	}
	el := buildElement("uia", "1234", "win1", p)
	if el.Role != core.RoleButton {
		t.Fatalf("Role = %q, want button", el.Role)
	}
	if el.Name != "OK" {
		t.Fatalf("Name = %q", el.Name)
	}
	if !el.Enabled || !el.Visible || !el.Focused {
		t.Fatalf("expected enabled/visible/focused true, got %+v", el)
	}
	if el.Backend != "uia" || el.AppID != "1234" || el.WindowID != "win1" {
		t.Fatalf("unexpected provenance: %+v", el)
	}
	if el.Native.Platform != "uia" || el.Native.Role != "button" {
		t.Fatalf("unexpected native data: %+v", el.Native)
	}
	if got := runtimeIDOf(el); got != "42.7" {
		t.Fatalf("runtimeIDOf = %q, want 42.7", got)
	}
}

func TestBuildElementOffscreen(t *testing.T) {
	el := buildElement("uia", "", "", cachedProps{ControlType: ControlTypeEdit, Offscreen: true})
	if el.Visible {
		t.Fatalf("expected Visible=false for an offscreen element")
	}
}

func TestRuntimeIDOfNil(t *testing.T) {
	if got := runtimeIDOf(nil); got != "" {
		t.Fatalf("runtimeIDOf(nil) = %q, want empty", got)
	}
	if got := runtimeIDOf(&core.Element{}); got != "" {
		t.Fatalf("runtimeIDOf(no native) = %q, want empty", got)
	}
}
