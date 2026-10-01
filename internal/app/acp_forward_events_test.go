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

// TestForwardACPAgentEventsMapsResumeMarkers: the "session resumed" and
// "delegated result injected" events become visible notices, each with its own
// messageId so Xcode does not merge them into a neighbouring assistant message.
// ConclusionQueued stays unmapped (broker-only, and "injected" follows it).
func TestForwardACPAgentEventsMapsResumeMarkers(t *testing.T) {
	in := make(chan agent.AgentEvent, 8)
	in <- agent.AgentEvent{Type: agent.AgentEventTypeResurrected, SessionID: "s1", SystemMessage: "resuming"}
	in <- agent.AgentEvent{Type: agent.AgentEventTypeConclusionQueued, SessionID: "s1", SystemMessage: "queued"}
	in <- agent.AgentEvent{Type: agent.AgentEventTypeConclusionInjected, SessionID: "s1", SystemMessage: "injected"}
	in <- agent.AgentEvent{Type: agent.AgentEventTypeResurrected, SessionID: "s1"} // empty text: nothing to show
	close(in)

	var got []mesnadaACP.AgentEvent
	for ev := range ForwardACPAgentEvents(context.Background(), in) {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 marker events, got %d: %#v", len(got), got)
	}
	for i, want := range []string{"resuming", "injected"} {
		if got[i].Type != mesnadaACP.AgentEventTypeSystemMessage || got[i].SystemMessage != want {
			t.Fatalf("event #%d = %#v, want system message %q", i, got[i], want)
		}
		if got[i].MessageID == "" {
			t.Fatalf("event #%d has no messageId", i)
		}
	}
	if got[0].MessageID == got[1].MessageID {
		t.Fatalf("markers share messageId %q", got[0].MessageID)
	}
}

// TestRegisterACPResumeHandlerDeclinesForeignSessions: with no live ACP session
// for the id, the ACP owner handler declines without starting the run, so the
// lower-priority surface (WebUI fallback) gets it; unregistering removes it.
func TestRegisterACPResumeHandlerDeclinesForeignSessions(t *testing.T) {
	reg := agent.NewResumeRegistry()
	acpAgent := mesnadaACP.NewPandoACPAgent("test", t.TempDir(), nil, nil, nil, nil)
	unregister := RegisterACPResumeHandler(reg, acpAgent)

	fallbackTook := false
	reg.Register(agent.ResumePriorityFallback, func(string, agent.ResumeStart) (bool, error) {
		fallbackTook = true
		return true, nil
	})
	started := false
	start := func(context.Context) (<-chan agent.AgentEvent, error) { started = true; return nil, nil }

	if taken, err := reg.Offer("no-acp-session", start); !taken || err != nil || !fallbackTook {
		t.Fatalf("taken=%v err=%v fallbackTook=%v, want the fallback to take it", taken, err, fallbackTook)
	}
	if started {
		t.Fatal("the ACP handler started a run for a session it does not own")
	}
	unregister()
	unregister() // safe to call twice
	if RegisterACPResumeHandler(nil, acpAgent) == nil {
		t.Fatal("nil registry must return a usable no-op unregister")
	}
}
