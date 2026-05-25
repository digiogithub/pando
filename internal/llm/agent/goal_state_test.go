package agent

import (
	"testing"
	"time"
)

func TestGoalStateAdvanceAndComplete(t *testing.T) {
	state := NewGoalState("goal-1", "session-1", "Finish phase one", 3, 600)

	state.Advance("Created DB schema", "Run a validation iteration")
	if state.Iteration != 1 {
		t.Fatalf("expected iteration 1, got %d", state.Iteration)
	}
	if state.LastProgress != "Created DB schema" {
		t.Fatalf("unexpected last progress: %q", state.LastProgress)
	}
	if state.NextStep != "Run a validation iteration" {
		t.Fatalf("unexpected next step: %q", state.NextStep)
	}

	now := time.Unix(100, 0)
	if err := state.Transition(GoalStatusCompleted, now); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if !state.IsTerminal() {
		t.Fatal("expected terminal state")
	}
	if state.CompletedAt == nil || !state.CompletedAt.Equal(now) {
		t.Fatalf("unexpected completed time: %+v", state.CompletedAt)
	}
}

func TestGoalStateInvalidTransition(t *testing.T) {
	state := NewGoalState("goal-2", "session-1", "Finish phase one", 3, 600)
	state.SetStatus(GoalStatusBlocked)

	if err := state.Transition(GoalStatusRunning, time.Unix(200, 0)); err == nil {
		t.Fatal("expected invalid transition error")
	}
}

func TestGoalStateCancel(t *testing.T) {
	state := NewGoalState("goal-3", "session-1", "Finish phase one", 3, 600)
	if state.IsCancelled() {
		t.Fatal("goal should not start cancelled")
	}

	state.Cancel()
	if !state.IsCancelled() {
		t.Fatal("goal should be cancelled")
	}
}

func TestGoalStateRecordIterationTracksStalls(t *testing.T) {
	state := NewGoalState("goal-4", "session-1", "Finish phase one", 3, 600)

	state.RecordIteration("Investigated failing test", "Run fix", "todo-1", "sig-1", true)
	if state.Iteration != 1 {
		t.Fatalf("expected iteration 1, got %d", state.Iteration)
	}
	if state.ConsecutiveNoProgress != 0 {
		t.Fatalf("expected stall counter reset, got %d", state.ConsecutiveNoProgress)
	}

	state.RecordIteration("Still blocked", "Run fix", "todo-1", "sig-1", false)
	if state.ConsecutiveNoProgress != 1 {
		t.Fatalf("expected stall counter 1, got %d", state.ConsecutiveNoProgress)
	}
	if state.LastTodoSignature != "todo-1" {
		t.Fatalf("unexpected last todo signature %q", state.LastTodoSignature)
	}
	if state.LastEvaluationSignature != "sig-1" {
		t.Fatalf("unexpected last evaluation signature %q", state.LastEvaluationSignature)
	}
}
