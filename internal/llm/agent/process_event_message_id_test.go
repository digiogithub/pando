package agent

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

// noopMessagesService is a minimal message.Service used only to satisfy
// processEvent's a.messages.Update call without a database, mirroring the
// pattern used by steeringMockMessages in steering_test.go.
type noopMessagesService struct {
	*pubsub.Broker[message.Message]
}

func (noopMessagesService) Create(ctx context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	return message.Message{}, nil
}

func (noopMessagesService) Update(ctx context.Context, msg message.Message) error { return nil }

func (noopMessagesService) Get(ctx context.Context, id string) (message.Message, error) {
	return message.Message{}, nil
}

func (noopMessagesService) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	return nil, nil
}

func (noopMessagesService) Delete(ctx context.Context, id string) error { return nil }

func (noopMessagesService) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	return nil
}

// TestProcessEventPopulatesMessageIDOnDeltas covers the ACP Xcode 27
// compatibility fix (PANDO-US-0064): ThinkingDelta and ContentDelta events must
// carry the in-flight assistant message's ID (assistantMsg.ID) from the very
// first delta, not only after the terminal AgentEventTypeResponse event, so
// ACP-side live chunks can be grouped under the same messageId used by
// session/load replay.
func TestProcessEventPopulatesMessageIDOnDeltas(t *testing.T) {
	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		messages: noopMessagesService{Broker: pubsub.NewBroker[message.Message]()},
	}

	assistantMsg := &message.Message{ID: "assistant-msg-1", Role: message.Assistant}
	eventCh := make(chan AgentEvent, 4)

	if err := a.processEvent(context.Background(), "session-1", assistantMsg, provider.ProviderEvent{
		Type:     provider.EventThinkingDelta,
		Thinking: "thinking...",
	}, nil, eventCh); err != nil {
		t.Fatalf("processEvent (ThinkingDelta) failed: %v", err)
	}
	if err := a.processEvent(context.Background(), "session-1", assistantMsg, provider.ProviderEvent{
		Type:    provider.EventContentDelta,
		Content: "hello",
	}, nil, eventCh); err != nil {
		t.Fatalf("processEvent (ContentDelta) failed: %v", err)
	}
	close(eventCh)

	var got []AgentEvent
	for ev := range eventCh {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events on eventCh, got %d: %#v", len(got), got)
	}
	if got[0].Type != AgentEventTypeThinkingDelta || got[0].MessageID != assistantMsg.ID {
		t.Fatalf("expected thinking delta event to carry messageId %q, got %#v", assistantMsg.ID, got[0])
	}
	if got[1].Type != AgentEventTypeContentDelta || got[1].MessageID != assistantMsg.ID {
		t.Fatalf("expected content delta event to carry messageId %q, got %#v", assistantMsg.ID, got[1])
	}
}
