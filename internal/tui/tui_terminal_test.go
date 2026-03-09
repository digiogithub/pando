package tui

import (
	"testing"

	"github.com/digiogithub/pando/internal/tui/components/terminal"
)

func TestDefaultKeyMapTerminalBindingsAreDistinct(t *testing.T) {
	keys := DefaultKeyMap()

	toggle := keys.Global.ToggleTerminal.Keys()
	focus := keys.Global.FocusTerminal.Keys()

	if len(toggle) != 1 || toggle[0] != "ctrl+`" {
		t.Fatalf("unexpected toggle terminal binding: %v", toggle)
	}
	if len(focus) != 1 || focus[0] != "alt+`" {
		t.Fatalf("unexpected focus terminal binding: %v", focus)
	}
	if toggle[0] == focus[0] {
		t.Fatalf("toggle and focus terminal bindings must be distinct, got %q", toggle[0])
	}
}

func TestTerminalFocusChangedMsgUpdatesFocusState(t *testing.T) {
	model := appModel{terminalPanel: terminal.NewTerminalPanel()}

	updated, _ := model.Update(terminalFocusChangedMsg{focused: true})
	got := updated.(appModel)
	if !got.terminalFocused {
		t.Fatal("expected terminalFocused to be true")
	}
	if !got.terminalPanel.IsFocused() {
		t.Fatal("expected terminal panel to be focused")
	}

	updated, _ = got.Update(terminalFocusChangedMsg{focused: false})
	got = updated.(appModel)
	if got.terminalFocused {
		t.Fatal("expected terminalFocused to be false")
	}
	if got.terminalPanel.IsFocused() {
		t.Fatal("expected terminal panel to be blurred")
	}
}