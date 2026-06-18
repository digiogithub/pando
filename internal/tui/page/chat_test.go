package page

import (
	"testing"

	agentpkg "github.com/digiogithub/pando/internal/llm/agent"
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

func TestWaitForGoalEventReportsClosedChannel(t *testing.T) {
	page := &ChatPageModel{}
	ch := make(chan agentpkg.AgentEvent, 1)
	cmd := page.waitForGoalEvent("session-1", ch)

	ch <- agentpkg.AgentEvent{Type: agentpkg.AgentEventTypeResponse, SessionID: "session-1"}
	msg, ok := cmd().(goalEventChanMsg)
	if !ok {
		t.Fatalf("expected goalEventChanMsg, got %T", msg)
	}
	if !msg.ok {
		t.Fatal("expected ok=true while the channel is open and delivering events")
	}
	if msg.sessionID != "session-1" {
		t.Fatalf("expected sessionID to be propagated, got %q", msg.sessionID)
	}

	// Closing the channel must surface ok=false so the TUI can reconcile the
	// terminal goal state (otherwise the input lock and Ctrl+C stay stuck).
	close(ch)
	closedMsg, ok := page.waitForGoalEvent("session-1", ch)().(goalEventChanMsg)
	if !ok {
		t.Fatalf("expected goalEventChanMsg, got %T", closedMsg)
	}
	if closedMsg.ok {
		t.Fatal("expected ok=false once the goal channel is closed")
	}
}
