package agui

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
)

// types collects the event type of each emitted event, which is what the
// protocol's ordering rules are expressed in.
func types(events []Event) []EventType {
	out := make([]EventType, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.EventType())
	}
	return out
}

func assertTypes(t *testing.T, got []Event, want ...EventType) {
	t.Helper()
	gotTypes := types(got)
	if len(gotTypes) != len(want) {
		t.Fatalf("got %v, want %v", gotTypes, want)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Fatalf("got %v, want %v", gotTypes, want)
		}
	}
}

func TestTranslatorTextLifecycle(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")

	assertTypes(t, tr.Start(), EventRunStarted)

	// The first delta opens the message; later deltas must not re-open it.
	assertTypes(t,
		tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "hel"}),
		EventTextMessageStart, EventTextMessageContent,
	)
	assertTypes(t,
		tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "lo"}),
		EventTextMessageContent,
	)
	// Empty deltas carry no information and must not produce frames.
	assertTypes(t, tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: ""}))

	assertTypes(t, tr.Finish(OutcomeSuccess, "hello"),
		EventTextMessageEnd, EventRunFinished,
	)
}

func TestTranslatorReasoningClosesBeforeText(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()

	assertTypes(t,
		tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeThinkingDelta, Delta: "thinking"}),
		EventReasoningStart, EventReasoningMessageStart, EventReasoningMessageContent,
	)
	// Switching to visible text must close the reasoning block first.
	assertTypes(t,
		tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "answer"}),
		EventReasoningMessageEnd, EventReasoningEnd,
		EventTextMessageStart, EventTextMessageContent,
	)
}

func TestTranslatorToolCallArgsAreIncremental(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()

	// Pando re-emits a growing snapshot of the same call; AG-UI wants deltas.
	assertTypes(t,
		tr.Translate(agent.AgentEvent{
			Type:     agent.AgentEventTypeToolCall,
			ToolCall: &message.ToolCall{ID: "tc1", Name: "bash", Input: `{"cmd`},
		}),
		EventToolCallStart, EventToolCallArgs,
	)

	got := tr.Translate(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "tc1", Name: "bash", Input: `{"cmd":"ls"}`},
	})
	assertTypes(t, got, EventToolCallArgs)
	args, ok := got[0].(ToolCallArgsEvent)
	if !ok {
		t.Fatalf("expected ToolCallArgsEvent, got %T", got[0])
	}
	if args.Delta != `":"ls"}` {
		t.Fatalf("expected only the new suffix, got %q", args.Delta)
	}

	// A repeated identical snapshot adds nothing.
	assertTypes(t, tr.Translate(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "tc1", Name: "bash", Input: `{"cmd":"ls"}`},
	}))

	assertTypes(t, tr.Translate(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "tc1", Name: "bash", Input: `{"cmd":"ls"}`, Finished: true},
	}), EventToolCallEnd)
}

func TestTranslatorToolResultClosesUnfinishedCall(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()
	tr.Translate(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "tc1", Name: "ls", Input: "{}"},
	})

	// The call never reported Finished, but a result implies it ended.
	assertTypes(t, tr.Translate(agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "tc1", Name: "ls", Content: "a\nb"},
	}), EventToolCallEnd, EventToolCallResult)
}

func TestTranslatorFinishClosesEverything(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()
	tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeThinkingDelta, Delta: "t"})
	tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "c"})
	tr.Translate(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "tc1", Name: "ls", Input: "{}"},
	})

	assertTypes(t, tr.Finish(OutcomeSuccess, "c"),
		EventToolCallEnd, EventTextMessageEnd, EventRunFinished,
	)
}

func TestTranslatorFailEmitsRunError(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()
	tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "partial"})

	got := tr.Fail("boom", "session_busy")
	assertTypes(t, got, EventTextMessageEnd, EventRunError)
	runErr, ok := got[1].(RunErrorEvent)
	if !ok {
		t.Fatalf("expected RunErrorEvent, got %T", got[1])
	}
	if runErr.Message != "boom" || runErr.Code != "session_busy" {
		t.Fatalf("unexpected error event: %+v", runErr)
	}
}

func TestTranslatorErrorEventClosesBlocks(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()
	tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "partial"})

	assertTypes(t, tr.Translate(agent.AgentEvent{
		Type:  agent.AgentEventTypeError,
		Error: errors.New("upstream failed"),
	}), EventTextMessageEnd, EventRunError)
}

func TestTranslatorPandoOnlyEventsBecomeCustom(t *testing.T) {
	tr := newTranslator("thread-1", "run-1")
	tr.Start()

	got := tr.Translate(agent.AgentEvent{
		Type:          agent.AgentEventTypeSystemMessage,
		SystemMessage: "compacting context",
	})
	assertTypes(t, got, EventCustom)
	custom := got[0].(CustomEvent)
	if custom.Name != "pando.system_message" {
		t.Fatalf("expected a namespaced custom event, got %q", custom.Name)
	}

	// Events with no payload emit nothing.
	assertTypes(t, tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeSystemMessage}))
	assertTypes(t, tr.Translate(agent.AgentEvent{Type: agent.AgentEventTypeTokenUsage}))
}

// TestEventWireFormat pins the serialized shape: the type discriminator and the
// camelCase field names are the actual contract with AG-UI clients.
func TestEventWireFormat(t *testing.T) {
	cases := []struct {
		name  string
		event Event
		want  map[string]any
	}{
		{
			name:  "run started",
			event: NewRunStarted("thread-1", "run-1"),
			want:  map[string]any{"type": "RUN_STARTED", "threadId": "thread-1", "runId": "run-1"},
		},
		{
			name:  "text content",
			event: NewTextMessageContent("m1", "hi"),
			want:  map[string]any{"type": "TEXT_MESSAGE_CONTENT", "messageId": "m1", "delta": "hi"},
		},
		{
			name:  "tool call start",
			event: NewToolCallStart("tc1", "bash", "m1"),
			want: map[string]any{
				"type": "TOOL_CALL_START", "toolCallId": "tc1",
				"toolCallName": "bash", "parentMessageId": "m1",
			},
		},
		{
			name:  "tool call result",
			event: NewToolCallResult("m2", "tc1", "output"),
			want: map[string]any{
				"type": "TOOL_CALL_RESULT", "messageId": "m2",
				"toolCallId": "tc1", "content": "output", "role": "tool",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.event)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Fatalf("field %q = %v, want %v (payload: %s)", k, got[k], want, raw)
				}
			}
			if _, ok := got["timestamp"]; !ok {
				t.Fatalf("expected a timestamp, got %s", raw)
			}
		})
	}
}
