package app

import (
	"bytes"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

func TestAssistantTextStreamerStreamsOnlyNewAssistantContent(t *testing.T) {
	var output bytes.Buffer
	streamer := newAssistantTextStreamer(&output, "session-1")

	events := []pubsub.Event[message.Message]{
		{
			Type: pubsub.CreatedEvent,
			Payload: message.Message{
				ID:        "assistant-1",
				SessionID: "session-1",
				Role:      message.Assistant,
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "assistant-1",
				SessionID: "session-1",
				Role:      message.Assistant,
				Parts:     []message.ContentPart{message.TextContent{Text: "Voy a investigar"}},
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "assistant-1",
				SessionID: "session-1",
				Role:      message.Assistant,
				Parts: []message.ContentPart{
					message.TextContent{Text: "Voy a investigar"},
					message.ToolCall{ID: "tool-1", Name: "kb_search_documents", Input: "{\n  \"query\": \"pando\"\n}"},
				},
			},
		},
		{
			Type: pubsub.CreatedEvent,
			Payload: message.Message{
				ID:        "tool-msg-1",
				SessionID: "session-1",
				Role:      message.Tool,
				Parts:     []message.ContentPart{message.ToolResult{ToolCallID: "tool-1", Content: "2 docs found"}},
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "assistant-2",
				SessionID: "session-1",
				Role:      message.Assistant,
				Parts:     []message.ContentPart{message.TextContent{Text: "Resumen final"}},
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "assistant-3",
				SessionID: "session-2",
				Role:      message.Assistant,
				Parts:     []message.ContentPart{message.TextContent{Text: "ignored"}},
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

	if got, want := output.String(), "Voy a investigar\n\n🔧 kb_search_documents {\"query\":\"pando\"}\n\n✓ kb_search_documents completed\n\nResumen final\n"; got != want {
		t.Fatalf("streamed output = %q, want %q", got, want)
	}
}

func TestAssistantTextStreamerIgnoresRepeatedSnapshots(t *testing.T) {
	var output bytes.Buffer
	streamer := newAssistantTextStreamer(&output, "session-1")

	msg := message.Message{
		ID:        "assistant-1",
		SessionID: "session-1",
		Role:      message.Assistant,
		Parts:     []message.ContentPart{message.TextContent{Text: "stream"}},
	}

	if err := streamer.ConsumeMessage(msg); err != nil {
		t.Fatalf("ConsumeMessage() error = %v", err)
	}
	if err := streamer.ConsumeMessage(msg); err != nil {
		t.Fatalf("ConsumeMessage() second call error = %v", err)
	}

	if got, want := output.String(), "stream"; got != want {
		t.Fatalf("streamed output = %q, want %q", got, want)
	}
}