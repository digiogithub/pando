package chat

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/session"
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

func TestEditorDisablesInputWhileGoalRunning(t *testing.T) {
	editor := &editorCmp{
		session:  session.Session{ID: "session-1"},
		textarea: CreateTextArea(nil),
	}
	editor.textarea.SetValue("draft")
	editor.textarea.SetCursor(len("draft"))

	model, cmd := editor.Update(GoalUpdatedMsg{
		SessionID: "session-1",
		Goal: &GoalState{
			Objective: "Finish TUI integration",
			Status:    "running",
		},
	})
	if cmd != nil {
		t.Fatalf("expected no command when entering goal mode, got %v", cmd)
	}

	updated := model.(*editorCmp)
	model, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected no send command while goal is running, got %v", cmd)
	}

	stillDisabled := model.(*editorCmp)
	if got := stillDisabled.textarea.Value(); got != "draft" {
		t.Fatalf("expected textarea content to remain unchanged, got %q", got)
	}
	if !stillDisabled.goalRunning() {
		t.Fatal("expected editor to remain in goal-running state")
	}
}
