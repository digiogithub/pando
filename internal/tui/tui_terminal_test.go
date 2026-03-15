package tui

import (
	"testing"

	"github.com/digiogithub/pando/internal/tui/components/terminal"
)

func TestDefaultKeyMapTerminalBinding(t *testing.T) {
	keys := DefaultKeyMap()

	toggle := keys.Global.ToggleTerminal.Keys()
	newTerm := keys.Global.NewTerminal.Keys()

	if len(toggle) != 1 || toggle[0] != "ctrl+shift+t" {
		t.Fatalf("unexpected toggle terminal binding: %v", toggle)
	}
	if len(newTerm) != 1 || newTerm[0] != "ctrl+alt+t" {
		t.Fatalf("unexpected new terminal binding: %v", newTerm)
	}
	if toggle[0] == newTerm[0] {
		t.Fatalf("toggle and new-terminal bindings must be distinct, got %q", toggle[0])
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
