package app

import (
	"bytes"
	"testing"

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
				SessionID: "session-1",
				Type:      agent.AgentEventTypeToolResult,
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