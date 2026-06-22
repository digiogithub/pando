package app

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
)

func TestFormatNonInteractiveGoalResult(t *testing.T) {
	serialized, err := formatNonInteractiveGoalResult(NonInteractiveGoalResult{
		SessionID:     "session-1",
		Objective:     "Ship goal mode",
		Status:        agent.GoalStatusCompleted,
		Iteration:     3,
		MaxIterations: 5,
		Response:      "Done",
	})
	if err != nil {
		t.Fatalf("formatNonInteractiveGoalResult() error = %v", err)
	}

	if strings.Contains(serialized, "\"status\"") {
		t.Fatalf("expected structured TOON/TOML output instead of raw JSON: %s", serialized)
	}
	if strings.Contains(serialized, "status = 'completed'") {
		t.Fatalf("expected TOON output instead of TOML, got: %s", serialized)
	}
	if !strings.Contains(serialized, "status: completed") {
		t.Fatalf("expected TOON-formatted status field, got: %s", serialized)
	}
	if !strings.Contains(serialized, "response: Done") {
		t.Fatalf("expected TOON-formatted response field, got: %s", serialized)
	}
	if strings.Contains(serialized, "blocked_reason") {
		t.Fatalf("blocked_reason should be omitted when empty: %s", serialized)
	}
}
