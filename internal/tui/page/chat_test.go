package page

import (
	"testing"

	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/components/chat"
)

func TestHasRunningGoalUsesPendingObjective(t *testing.T) {
	page := &ChatPageModel{goalObjectivePending: "Finish TUI goal wiring"}

	if !page.HasRunningGoal() {
		t.Fatal("expected pending goal objective to mark goal as running")
	}
}

func TestApplyGoalUpdateClearsPendingObjectiveOnRunningGoal(t *testing.T) {
	page := &ChatPageModel{
		session:              session.Session{ID: "session-1"},
		goalObjectivePending: "Finish TUI goal wiring",
	}

	cmd := page.applyGoalUpdate(chat.GoalUpdatedMsg{
		SessionID: "other-session",
		Goal: &chat.GoalState{
			Objective: "Finish TUI goal wiring",
			Status:    "running",
		},
	})
	if cmd != nil {
		t.Fatalf("expected no command for non-session goal update, got %v", cmd)
	}
	if page.goalObjectivePending != "Finish TUI goal wiring" {
		t.Fatalf("expected pending objective to remain when session does not match, got %q", page.goalObjectivePending)
	}

	cmd = page.applyGoalUpdate(chat.GoalUpdatedMsg{
		SessionID: "session-1",
		Goal: &chat.GoalState{
			Objective: "Finish TUI goal wiring",
			Status:    "running",
		},
	})
	if cmd != nil {
		t.Fatalf("expected no status command while goal is still running, got %v", cmd)
	}
	if page.goalObjectivePending != "" {
		t.Fatalf("expected pending objective to clear once goal starts running, got %q", page.goalObjectivePending)
	}
}
