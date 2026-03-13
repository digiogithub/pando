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
				Parts:     []message.ContentPart{message.TextContent{Text: "Ho"}},
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "assistant-1",
				SessionID: "session-1",
				Role:      message.Assistant,
				Parts:     []message.ContentPart{message.TextContent{Text: "Hola"}},
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "user-1",
				SessionID: "session-1",
				Role:      message.User,
				Parts:     []message.ContentPart{message.TextContent{Text: "ignored"}},
			},
		},
		{
			Type: pubsub.UpdatedEvent,
			Payload: message.Message{
				ID:        "assistant-2",
				SessionID: "session-1",
				Role:      message.Assistant,
				Parts:     []message.ContentPart{message.TextContent{Text: " mundo"}},
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

	if got, want := output.String(), "Hola mundo\n"; got != want {
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