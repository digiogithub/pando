package agent

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
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

func TestSanitizeToolCallHistory_MergesConsecutiveToolMessagesAndSynthesizesMissingResults(t *testing.T) {
	msgs := []message.Message{
		{
			ID:   "assistant-1",
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "toolu_a", Name: "write", Input: `{"file_path":"a.txt"}`},
				message.ToolCall{ID: "toolu_b", Name: "write", Input: `{"file_path":"b.txt"}`},
			},
		},
		{
			ID:   "tool-1",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "toolu_a", Name: "write", Content: "ok"},
			},
		},
		{
			ID:   "tool-2",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "toolu_b", Name: "write", Content: "late result"},
				message.ToolResult{ToolCallID: "toolu_a", Name: "write", Content: "duplicate"},
				message.ToolResult{ToolCallID: "toolu_orphan", Name: "write", Content: "orphan"},
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
		t.Fatalf("expected merged tool message at index 1, got %q", got[1].Role)
	}

	results := got[1].ToolResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 normalized tool results, got %d", len(results))
	}
	if results[0].ToolCallID != "toolu_a" || results[0].Content != "ok" {
		t.Fatalf("expected first tool result for toolu_a to preserve earliest content, got %+v", results[0])
	}
	if results[1].ToolCallID != "toolu_b" || results[1].Content != "late result" {
		t.Fatalf("expected second tool result for toolu_b, got %+v", results[1])
	}
	if got[2].Role != message.User {
		t.Fatalf("expected trailing user message to remain, got %q", got[2].Role)
	}
}

func TestSanitizeToolCallHistory_DropsLeadingOrphanToolMessage(t *testing.T) {
	msgs := []message.Message{
		{
			ID:   "tool-1",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "toolu_orphan", Name: "write", Content: "orphan"},
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
	if len(got) != 1 {
		t.Fatalf("expected orphan tool message to be dropped, got %d messages", len(got))
	}
	if got[0].Role != message.User {
		t.Fatalf("expected remaining message to be user, got %q", got[0].Role)
	}
}

func TestFitMessagesToProviderBudget_DropsOrphanToolMessageCreatedByFrontTrim(t *testing.T) {
	t.Cleanup(func() {
		config.SetForTests(nil)
	})
	config.SetForTests(&config.Config{
		Agents: map[config.AgentName]config.Agent{
			config.AgentCoder: {},
		},
	})

	msgs := []message.Message{
		{
			ID:   "assistant-1",
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "toolu_1", Name: "write", Input: `{"file_path":"a.txt","content":"abcdefghijklmnopqrstuvwxyz"}`},
			},
		},
		{
			ID:   "tool-1",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "toolu_1", Name: "write", Content: "ok"},
			},
		},
		{
			ID:   "user-1",
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: "abcdefghijklmnopqrstuvwxyz"},
			},
		},
	}

	model := models.Model{
		ID:               "test-model",
		ContextWindow:    16,
		DefaultMaxTokens: 1,
	}

	got := fitMessagesToProviderBudget(msgs, config.AgentCoder, model)
	if len(got) != 1 {
		t.Fatalf("expected only the user message to remain after trim+s sanitize, got %d messages", len(got))
	}
	if got[0].Role != message.User {
		t.Fatalf("expected trimmed history to drop orphan tool message, got %q", got[0].Role)
	}
}

func TestSummaryNeedsContinuationPrompt_WhenHistoryEndsWithToolResults(t *testing.T) {
	msgs := []message.Message{
		{
			ID:   "assistant-1",
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "toolu_1", Name: "write", Input: `{"file_path":"a.txt"}`},
			},
		},
		{
			ID:   "tool-1",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "toolu_1", Name: "write", Content: "ok"},
			},
		},
	}

	if !summaryNeedsContinuationPrompt(msgs) {
		t.Fatal("expected compaction summary to include continuation marker after tool results")
	}
}

func TestSummaryNeedsContinuationPrompt_WhenConversationAlreadyContinued(t *testing.T) {
	msgs := []message.Message{
		{
			ID:   "assistant-1",
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.ToolCall{ID: "toolu_1", Name: "write", Input: `{"file_path":"a.txt"}`},
			},
		},
		{
			ID:   "tool-1",
			Role: message.Tool,
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "toolu_1", Name: "write", Content: "ok"},
			},
		},
		{
			ID:   "user-1",
			Role: message.User,
			Parts: []message.ContentPart{
				message.TextContent{Text: "thanks"},
			},
		},
	}

	if summaryNeedsContinuationPrompt(msgs) {
		t.Fatal("expected no continuation marker once the conversation has already moved past the tool loop")
	}
}
