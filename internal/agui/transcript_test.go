package agui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/message"
)

// TestToAGUIMessages_ToolCallAndResultPairingIntact is the shared-converter
// half of the PANDO-US-0015/0016 acceptance criteria: toolCalls/toolCallId
// are preserved and assistant/tool pairing survives the conversion.
func TestToAGUIMessages_ToolCallAndResultPairingIntact(t *testing.T) {
	msgs := []message.Message{
		{ID: "m1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "run it"}}},
		{ID: "m2", Role: message.Assistant, Parts: []message.ContentPart{
			message.ToolCall{ID: "call-1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
		}},
		{ID: "m3", Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "call-1", Name: "bash", Content: "file.go"},
		}},
	}
	got := toAGUIMessages(msgs)
	if len(got) != 3 {
		t.Fatalf("expected 3 AG-UI messages, got %d: %+v", len(got), got)
	}
	if got[0].Role != RoleUser || got[0].Content.Text != "run it" {
		t.Fatalf("user message not preserved: %+v", got[0])
	}
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call-1" ||
		got[1].ToolCalls[0].Function.Name != "bash" || got[1].ToolCalls[0].Function.Arguments != `{"command":"ls"}` {
		t.Fatalf("assistant tool call not preserved: %+v", got[1])
	}
	if got[2].Role != RoleTool || got[2].ToolCallID != "call-1" || got[2].Content.Text != "file.go" {
		t.Fatalf("tool result did not carry the matching toolCallId: %+v", got[2])
	}
}

// TestToAGUIMessages_ExpandsMultiResultToolMessage: a Pando "tool" message can
// carry more than one ToolResult (one per call resolved in the same turn);
// AG-UI expects one message per tool result.
func TestToAGUIMessages_ExpandsMultiResultToolMessage(t *testing.T) {
	msgs := []message.Message{
		{ID: "m1", Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "call-1", Name: "a", Content: "one"},
			message.ToolResult{ToolCallID: "call-2", Name: "b", Content: "two"},
		}},
	}
	got := toAGUIMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected one AG-UI message per tool result, got %d: %+v", len(got), got)
	}
	if got[0].ToolCallID != "call-1" || got[1].ToolCallID != "call-2" {
		t.Fatalf("tool call ids not preserved in order: %+v", got)
	}
}

// TestToAGUIMessages_RoleMapping pins the role vocabulary translation.
func TestToAGUIMessages_RoleMapping(t *testing.T) {
	msgs := []message.Message{
		{ID: "u", Role: message.User},
		{ID: "a", Role: message.Assistant},
		{ID: "s", Role: message.System},
	}
	got := toAGUIMessages(msgs)
	want := []string{RoleUser, RoleAssistant, RoleSystem}
	for i, w := range want {
		if got[i].Role != w {
			t.Fatalf("message %d role = %q, want %q", i, got[i].Role, w)
		}
	}
}

func TestCapMessages_KeepsMostRecentWithinCount(t *testing.T) {
	msgs := []Message{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}}
	kept, truncated := capMessages(msgs, 2, 0)
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if len(kept) != 2 || kept[0].ID != "4" || kept[1].ID != "5" {
		t.Fatalf("expected the 2 most recent messages, got %+v", kept)
	}
}

func TestCapMessages_NoLimitsMeansNoTruncation(t *testing.T) {
	msgs := []Message{{ID: "1"}, {ID: "2"}}
	kept, truncated := capMessages(msgs, 0, 0)
	if truncated || len(kept) != 2 {
		t.Fatalf("expected no truncation with no limits configured, got %+v truncated=%v", kept, truncated)
	}
}

// TestCapMessages_ByteCapKeepsAtLeastNewestMessage: even a single message
// that alone exceeds the byte budget must not produce an empty snapshot.
func TestCapMessages_ByteCapKeepsAtLeastNewestMessage(t *testing.T) {
	msgs := []Message{
		{ID: "1", Content: MessageContent{Text: strings.Repeat("x", 500)}},
		{ID: "2", Content: MessageContent{Text: "small"}},
	}
	kept, truncated := capMessages(msgs, 0, 10) // smaller than either message alone
	if len(kept) != 1 || kept[0].ID != "2" {
		t.Fatalf("expected only the newest message kept, got %+v", kept)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}
}

// TestCapMessages_ByteCapStopsBeforeBlowingBudget confirms the byte cap
// drops whole messages from the oldest end rather than truncating one
// mid-message.
func TestCapMessages_ByteCapStopsBeforeBlowingBudget(t *testing.T) {
	msgs := []Message{
		{ID: "1", Content: MessageContent{Text: strings.Repeat("a", 100)}},
		{ID: "2", Content: MessageContent{Text: strings.Repeat("b", 20)}},
	}
	budget := messageByteSize(msgs[1]) + 5 // room for msg 2 alone, not both
	kept, truncated := capMessages(msgs, 0, budget)
	if len(kept) != 1 || kept[0].ID != "2" {
		t.Fatalf("expected only message 2 kept, got %+v", kept)
	}
	if !truncated {
		t.Fatal("expected truncated=true")
	}
}

// TestBuildMessagesSnapshot_TruncationFlaggedAndMostRecentFirstComplete is
// the PANDO-US-0016 acceptance criterion in its purest form: the message-count
// cap keeps the newest messages complete and flags the result as truncated.
func TestBuildMessagesSnapshot_TruncationFlaggedAndMostRecentFirstComplete(t *testing.T) {
	msgs := make([]message.Message, 0, 5)
	for i := 1; i <= 5; i++ {
		msgs = append(msgs, message.Message{
			ID: fmt.Sprintf("m%d", i), Role: message.User,
			Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("msg %d", i)}},
		})
	}
	snap := buildMessagesSnapshot(msgs, 2, 0)
	if !snap.Truncated {
		t.Fatal("expected Truncated=true")
	}
	if len(snap.Messages) != 2 || snap.Messages[0].ID != "m4" || snap.Messages[1].ID != "m5" {
		t.Fatalf("expected the 2 most recent messages complete, got %+v", snap.Messages)
	}
}

// TestBuildMessagesSnapshot_UnderCapIsNotTruncated: a history within the cap
// must not be flagged, so the client does not treat a full transcript as
// partial.
func TestBuildMessagesSnapshot_UnderCapIsNotTruncated(t *testing.T) {
	msgs := []message.Message{
		{ID: "m1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
	}
	snap := buildMessagesSnapshot(msgs, 10, 0)
	if snap.Truncated {
		t.Fatal("a history under the cap must not be flagged truncated")
	}
	if len(snap.Messages) != 1 {
		t.Fatalf("expected 1 message, got %+v", snap.Messages)
	}
}
