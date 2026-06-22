package agent

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/message"
)

func TestInjectConclusionRejectsWhenNotBusy(t *testing.T) {
	a := newSteeringTestAgent()
	err := a.InjectConclusion("s1", "[delegated-result ...] summary: done")
	if err != ErrSessionNotBusy {
		t.Fatalf("expected ErrSessionNotBusy, got %v", err)
	}
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected 0 pending, got %d", got)
	}
}

func TestInjectConclusionRejectsEmptyContent(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")
	if err := a.InjectConclusion("s1", "   "); err == nil {
		t.Fatal("expected error for empty content")
	}
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected 0 pending, got %d", got)
	}
}

func TestInjectConclusionQueuesWhenBusyAndEmitsEvent(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")

	sub := a.Subscribe(context.Background())

	if err := a.InjectConclusion("s1", "[delegated-result task=t1] summary: done"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := a.PendingSteering("s1"); got != 1 {
		t.Fatalf("expected 1 pending, got %d", got)
	}

	select {
	case ev := <-sub:
		if ev.Payload.Type != AgentEventTypeConclusionQueued {
			t.Fatalf("expected ConclusionQueued event, got %v", ev.Payload.Type)
		}
	default:
		t.Fatal("expected a ConclusionQueued event to be published")
	}
}

func TestDrainInjectsConclusionAsUserMessageAndEmitsConclusionInjected(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")

	if err := a.InjectConclusion("s1", "[delegated-result task=t1] summary: done"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := []message.Message{{ID: "u0", Role: message.User}}
	eventCh := make(chan AgentEvent, 8)
	out, injected := a.drainSteeringInto(context.Background(), "s1", base, eventCh)
	if !injected {
		t.Fatal("expected injected=true")
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 messages after injection, got %d", len(out))
	}
	if out[1].Role != message.User {
		t.Fatalf("expected injected conclusion to be a User message, got %v", out[1].Role)
	}

	// The conclusion-specific event must be delivered on the run channel.
	var sawConclusion bool
	for {
		select {
		case ev := <-eventCh:
			if ev.Type == AgentEventTypeConclusionInjected {
				sawConclusion = true
			}
			if ev.Type == AgentEventTypeSteeringInjected {
				t.Fatal("did not expect a feedback SteeringInjected event for a conclusion-only drain")
			}
			continue
		default:
		}
		break
	}
	if !sawConclusion {
		t.Fatal("expected AgentEventTypeConclusionInjected on the run channel")
	}
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected queue drained, got %d", got)
	}
}

func TestDrainMixedKindsEmitsBothEvents(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")

	if err := a.Steer("s1", "user feedback"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := a.InjectConclusion("s1", "[delegated-result task=t1] summary: done"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := []message.Message{{ID: "u0", Role: message.User}}
	eventCh := make(chan AgentEvent, 8)
	out, injected := a.drainSteeringInto(context.Background(), "s1", base, eventCh)
	if !injected || len(out) != 3 {
		t.Fatalf("expected both injected (3 msgs), got injected=%v len=%d", injected, len(out))
	}

	var sawFeedback, sawConclusion bool
	for {
		select {
		case ev := <-eventCh:
			switch ev.Type {
			case AgentEventTypeSteeringInjected:
				sawFeedback = true
			case AgentEventTypeConclusionInjected:
				sawConclusion = true
			}
			continue
		default:
		}
		break
	}
	if !sawFeedback || !sawConclusion {
		t.Fatalf("expected both events; feedback=%v conclusion=%v", sawFeedback, sawConclusion)
	}
}
