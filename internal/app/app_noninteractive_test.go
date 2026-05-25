package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

func TestAssistantTextStreamerShowsToolsAndOnlyVisibleContent(t *testing.T) {
	var output bytes.Buffer
	streamer := newAssistantTextStreamer(&output, "session-1")

	events := []pubsub.Event[agent.AgentEvent]{
		{
			Type: pubsub.CreatedEvent,
			Payload: agent.AgentEvent{
				SessionID: "session-1",
				Type:      agent.AgentEventTypeToolCall,
				ToolCall:  &message.ToolCall{ID: "tool-1", Name: "kb_search_documents", Input: "{\n  \"query\": \"pando\"\n}"},
			},
		},
		{
			Type: pubsub.CreatedEvent,
			Payload: agent.AgentEvent{
				SessionID:  "session-1",
				Type:       agent.AgentEventTypeToolResult,
				ToolResult: &message.ToolResult{ToolCallID: "tool-1", Name: "kb_search_documents", Content: "2 docs found"},
			},
		},
		{
			Type: pubsub.CreatedEvent,
			Payload: agent.AgentEvent{
				SessionID: "session-1",
				Type:      agent.AgentEventTypeContentDelta,
				Delta:     "Resumen ",
			},
		},
		{
			Type: pubsub.CreatedEvent,
			Payload: agent.AgentEvent{
				SessionID: "session-1",
				Type:      agent.AgentEventTypeContentDelta,
				Delta:     "final",
			},
		},
		{
			Type: pubsub.CreatedEvent,
			Payload: agent.AgentEvent{
				SessionID: "session-2",
				Type:      agent.AgentEventTypeContentDelta,
				Delta:     "ignored",
			},
		},
	}

	for _, event := range events {
		if err := streamer.Consume(event); err != nil {
			t.Fatalf("Consume() error = %v", err)
		}
	}

	if err := streamer.CloseLine(); err != nil {
		t.Fatalf("CloseLine() error = %v", err)
	}

	if got, want := output.String(), "🔧 kb_search_documents {\"query\":\"pando\"}\n✓ kb_search_documents completed\n\nResumen final\n"; got != want {
		t.Fatalf("streamed output = %q, want %q", got, want)
	}
}

func TestAssistantTextStreamerPrintsFinalContentFallback(t *testing.T) {
	var output bytes.Buffer
	streamer := newAssistantTextStreamer(&output, "session-1")

	if err := streamer.PrintFinalContent("final answer"); err != nil {
		t.Fatalf("PrintFinalContent() error = %v", err)
	}
	if err := streamer.PrintFinalContent("ignored duplicate"); err != nil {
		t.Fatalf("PrintFinalContent() second call error = %v", err)
	}
	if err := streamer.CloseLine(); err != nil {
		t.Fatalf("CloseLine() error = %v", err)
	}

	if got, want := output.String(), "final answer\n"; got != want {
		t.Fatalf("streamed output = %q, want %q", got, want)
	}
}

func TestNonInteractiveGoalResultFromDB(t *testing.T) {
	goal := db.SessionGoal{
		SessionID:          "session-1",
		Objective:          "Ship goal mode",
		Status:             agent.GoalStatusBlocked,
		Iteration:          2,
		MaxIterations:      5,
		MaxDurationSeconds: 3600,
		LastProgress:       sql.NullString{String: "  Waiting on credentials  ", Valid: true},
		NextStep:           sql.NullString{String: "  Retry after auth  ", Valid: true},
		BlockedReason:      sql.NullString{String: "  Missing credentials  ", Valid: true},
	}

	got := nonInteractiveGoalResultFromDB(goal, "  Latest assistant summary  ")
	if got.SessionID != "session-1" || got.Objective != "Ship goal mode" {
		t.Fatalf("unexpected identity fields: %+v", got)
	}
	if got.Response != "Latest assistant summary" {
		t.Fatalf("Response = %q, want trimmed value", got.Response)
	}
	if got.Progress != "Waiting on credentials" {
		t.Fatalf("Progress = %q, want trimmed value", got.Progress)
	}
	if got.NextStep != "Retry after auth" {
		t.Fatalf("NextStep = %q, want trimmed value", got.NextStep)
	}
	if got.BlockedReason != "Missing credentials" {
		t.Fatalf("BlockedReason = %q, want trimmed value", got.BlockedReason)
	}
}

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

	var decoded map[string]any
	if err := json.Unmarshal([]byte(serialized), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded["status"] != agent.GoalStatusCompleted {
		t.Fatalf("status = %v, want %q", decoded["status"], agent.GoalStatusCompleted)
	}
	if decoded["response"] != "Done" {
		t.Fatalf("response = %v, want %q", decoded["response"], "Done")
	}
	if _, ok := decoded["blocked_reason"]; ok {
		t.Fatalf("blocked_reason should be omitted when empty: %v", decoded)
	}
}
