package agent

import (
	"testing"

	"github.com/digiogithub/pando/internal/message"
)

func TestResolveToolCallsOnComplete_PreservesStreamedOnToolUse(t *testing.T) {
	existing := []message.ToolCall{{
		ID:       "call_1",
		Name:     "apply_patch",
		Input:    "{\"foo\":\"bar\"}",
		Type:     "function",
		Finished: true,
	}}

	got := resolveToolCallsOnComplete(existing, nil, message.FinishReasonToolUse)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got))
	}
	if got[0].ID != "call_1" {
		t.Fatalf("expected preserved tool call ID call_1, got %q", got[0].ID)
	}
}

func TestResolveToolCallsOnComplete_PrefersResponseToolCalls(t *testing.T) {
	existing := []message.ToolCall{{ID: "streamed", Name: "old_tool"}}
	fromResponse := []message.ToolCall{{ID: "final", Name: "edit_file"}}

	got := resolveToolCallsOnComplete(existing, fromResponse, message.FinishReasonToolUse)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got))
	}
	if got[0].ID != "final" {
		t.Fatalf("expected response tool call ID final, got %q", got[0].ID)
	}
}

func TestResolveToolCallsOnComplete_DoesNotPreserveWhenNotToolUse(t *testing.T) {
	existing := []message.ToolCall{{ID: "call_1", Name: "apply_patch"}}

	got := resolveToolCallsOnComplete(existing, nil, message.FinishReasonEndTurn)
	if len(got) != 0 {
		t.Fatalf("expected 0 tool calls, got %d", len(got))
	}
}
