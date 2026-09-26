package app

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
	mesnadaACP "github.com/digiogithub/pando/internal/mesnada/acp"
)

// TestForwardACPAgentEventsKeepsMessageID guards the Xcode regression where the
// `pando acp` adapter dropped MessageID: Xcode groups chunks by messageId, so a
// missing id merged answers and prevented de-duplicating session/load replay.
func TestForwardACPAgentEventsKeepsMessageID(t *testing.T) {
	in := make(chan agent.AgentEvent, 4)
	in <- agent.AgentEvent{Type: agent.AgentEventTypeThinkingDelta, Delta: "t", MessageID: "m1"}
	in <- agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "c", MessageID: "m1"}
	in <- agent.AgentEvent{Type: agent.AgentEventTypeSystemMessage, SystemMessage: "notice"}
	close(in)

	var got []mesnadaACP.AgentEvent
	for ev := range ForwardACPAgentEvents(context.Background(), in) {
		got = append(got, ev)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 forwarded events, got %d: %#v", len(got), got)
	}
	for i, ev := range got[:2] {
		if ev.MessageID != "m1" {
			t.Fatalf("event #%d (%v): expected MessageID m1, got %q", i, ev.Type, ev.MessageID)
		}
	}
	if got[2].Type != mesnadaACP.AgentEventTypeSystemMessage || got[2].SystemMessage != "notice" {
		t.Fatalf("expected system message forwarded, got %#v", got[2])
	}
}
