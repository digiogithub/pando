package provider

import (
	"testing"

	"github.com/digiogithub/pando/internal/message"
)

func TestSanitizeToolCallArguments(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"valid object":    {`{"file_path":"a.go"}`, `{"file_path":"a.go"}`},
		"empty":           {"", "{}"},
		"bare string":     {`"hello"`, "{}"},
		"array":           {`[1,2]`, "{}"},
		"null":            {"null", "{}"},
		"garbage":         {"not json at all {{{", "{}"},
		"truncated write": {`{"file_path":"a.go","content":"package main\nfunc`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizeToolCallArguments("write", tc.in)
			if m := sanitizeToolCallArgumentsMap("write", tc.in); m == nil {
				t.Fatalf("map must never be nil")
			}
			if tc.want == "" {
				// Repaired or replaced, it must decode as an object.
				if sanitizeToolCallArgumentsMap("write", tc.in) == nil {
					t.Fatalf("expected object, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDropTruncatedToolCalls(t *testing.T) {
	calls := []message.ToolCall{
		{ID: "1", Name: "view", Input: `{"file_path":"a"}`},
		{ID: "2", Name: "write", Input: `{"file_path":"b","content":"trunc`},
		{ID: "3", Name: "ls", Input: ""},
	}
	got := dropTruncatedToolCalls(calls)
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "3" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestDropIncompleteToolCalls(t *testing.T) {
	calls := []message.ToolCall{
		{ID: "1", Name: "view", Input: `{"file_path":"a"}`},
		{ID: "", Name: "bash", Input: `{"command":"ls"}`},
		{ID: "3", Name: "", Input: `{"command":"ls"}`},
		{ID: "4", Name: "ls", Input: ""},
	}
	got := dropIncompleteToolCalls(calls)
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "4" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// historyWithIncompleteToolCall is a session poisoned by a tool call stored
// without ID and name, followed by its tool result and a new user turn.
func historyWithIncompleteToolCall() []message.Message {
	return []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "go"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "ok", Name: "view", Input: `{"file_path":"a"}`},
			message.ToolCall{ID: "", Name: "", Input: `{"command":"ls"}`},
		}},
		{Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "ok", Content: "file"},
			message.ToolResult{ToolCallID: "", Content: "tool not found", IsError: true},
		}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "bad", Name: "", Input: `{}`},
		}},
		{Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "bad", Content: "tool not found", IsError: true},
		}},
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "are you alive?"}}},
	}
}

func TestCopilotConvertMessagesSkipsIncompleteToolCalls(t *testing.T) {
	c := &copilotClient{}
	msgs := c.convertMessages(historyWithIncompleteToolCall())

	var assistants, tools int
	for _, m := range msgs {
		if m.OfAssistant != nil {
			assistants++
			for _, call := range m.OfAssistant.ToolCalls {
				if call.ID == "" || call.Function.Name == "" {
					t.Fatalf("incomplete tool call replayed: %+v", call)
				}
			}
		}
		if m.OfTool != nil {
			tools++
			if m.OfTool.ToolCallID != "ok" {
				t.Fatalf("orphan tool result replayed: %q", m.OfTool.ToolCallID)
			}
		}
	}
	// The assistant message left with no content and no valid call is dropped.
	if assistants != 1 || tools != 1 {
		t.Fatalf("got %d assistant and %d tool messages, want 1 and 1", assistants, tools)
	}
}

func TestCopilotResponsesInputSkipsIncompleteToolCalls(t *testing.T) {
	c := &copilotClient{}
	input := c.convertMessagesToResponsesInput(historyWithIncompleteToolCall())

	var calls, outputs int
	for _, item := range input {
		if item.OfFunctionCall != nil {
			calls++
			if item.OfFunctionCall.CallID != "ok" || item.OfFunctionCall.Name != "view" {
				t.Fatalf("unexpected function call: %+v", item.OfFunctionCall)
			}
		}
		if item.OfFunctionCallOutput != nil {
			outputs++
			if item.OfFunctionCallOutput.CallID != "ok" {
				t.Fatalf("orphan function call output: %q", item.OfFunctionCallOutput.CallID)
			}
		}
	}
	if calls != 1 || outputs != 1 {
		t.Fatalf("got %d calls and %d outputs, want 1 and 1", calls, outputs)
	}
}
