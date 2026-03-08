package chat

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditorCtrlJInsertsNewLine(t *testing.T) {
	editor := &editorCmp{textarea: CreateTextArea(nil)}
	editor.textarea.SetValue("hello")
	editor.textarea.SetCursor(len("hello"))

	model, cmd := editor.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if cmd != nil {
		t.Fatalf("expected no command, got %v", cmd)
	}

	updated := model.(*editorCmp)
	if got := updated.textarea.Value(); got != "hello\n" {
		t.Fatalf("expected newline to be inserted, got %q", got)
	}
}
