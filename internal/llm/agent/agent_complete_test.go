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

func TestSanitizeToolCallHistory_InsertsSyntheticResultWhenAssistantToolCallIsNotImmediatelyFollowedByToolMessage(t *testing.T) {
	msgs := []message.Message{
		{
			ID:   "assistant-1",
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "toolu_123", Name: "kb_add_document", Input: `{"file_path":"doc.md","content":"hello"}`},
			},
		},
		{
			ID:   "user-1",
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: "continue"},
			},
		},
	}

	got := sanitizeToolCallHistory(msgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 messages after sanitization, got %d", len(got))
	}
	if got[1].Role != message.Tool {
		t.Fatalf("expected inserted message at index 1 to be tool role, got %q", got[1].Role)
	}
	results := got[1].ToolResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 synthetic tool result, got %d", len(results))
	}
	if results[0].ToolCallID != "toolu_123" {
		t.Fatalf("expected synthetic tool result for toolu_123, got %q", results[0].ToolCallID)
	}
	if !results[0].IsError {
		t.Fatal("expected synthetic tool result to be marked as error")
	}
	if got[2].Role != message.User {
		t.Fatalf("expected original user message to remain after inserted tool result, got %q", got[2].Role)
	}
}
