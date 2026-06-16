package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

// steeringMockMessages is a minimal message.Service used to exercise the
// steering queue logic without a database.
type steeringMockMessages struct {
	*pubsub.Broker[message.Message]
	mu      sync.Mutex
	created []message.Message
	seq     int
}

func newSteeringMockMessages() *steeringMockMessages {
	return &steeringMockMessages{Broker: pubsub.NewBroker[message.Message]()}
}

func (m *steeringMockMessages) Create(ctx context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	msg := message.Message{
		ID:        fmt.Sprintf("msg-%d", m.seq),
		SessionID: sessionID,
		Role:      params.Role,
		Parts:     params.Parts,
	}
	m.created = append(m.created, msg)
	return msg, nil
}

func (m *steeringMockMessages) Update(ctx context.Context, msg message.Message) error { return nil }
func (m *steeringMockMessages) Get(ctx context.Context, id string) (message.Message, error) {
	return message.Message{}, fmt.Errorf("not found")
}
func (m *steeringMockMessages) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	return nil, nil
}
func (m *steeringMockMessages) Delete(ctx context.Context, id string) error { return nil }
func (m *steeringMockMessages) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	return nil
}

// newSteeringTestAgent builds an agent with only the fields required by the
// steering queue methods.
func newSteeringTestAgent() *agent {
	return &agent{
		Broker:            pubsub.NewBroker[AgentEvent](),
		steeringQueue:     make(map[string][]steeringMessage),
		runStatusMessages: make(map[string][]string),
		messages:          newSteeringMockMessages(),
	}
}

// markBusy registers a no-op active request so IsSessionBusy reports true.
func (a *agent) markBusy(sessionID string) {
	_, cancel := context.WithCancel(context.Background())
	a.activeRequests.Store(sessionID, cancel)
}

func TestSteerRejectsWhenNotBusy(t *testing.T) {
	a := newSteeringTestAgent()
	err := a.Steer("s1", "do this instead")
	if err != ErrSessionNotBusy {
		t.Fatalf("expected ErrSessionNotBusy, got %v", err)
	}
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected 0 pending, got %d", got)
	}
}

func TestSteerRejectsEmptyContent(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")
	if err := a.Steer("s1", "   "); err == nil {
		t.Fatal("expected error for empty content")
	}
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected 0 pending, got %d", got)
	}
}

func TestSteerQueuesWhenBusy(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")

	if err := a.Steer("s1", "first feedback"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := a.Steer("s1", "second feedback"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := a.PendingSteering("s1"); got != 2 {
		t.Fatalf("expected 2 pending, got %d", got)
	}
	// Other sessions are unaffected.
	if got := a.PendingSteering("s2"); got != 0 {
		t.Fatalf("expected 0 pending for s2, got %d", got)
	}
}

func TestClearSteeringEmptiesQueue(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")
	_ = a.Steer("s1", "feedback")
	a.clearSteering("s1")
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected queue cleared, got %d", got)
	}
}

func TestCancelClearsSteeringQueue(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")
	_ = a.Steer("s1", "feedback")
	a.Cancel("s1")
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected queue cleared on cancel, got %d", got)
	}
}

func TestDrainSteeringIntoInjectsUserMessages(t *testing.T) {
	a := newSteeringTestAgent()
	a.markBusy("s1")
	_ = a.Steer("s1", "first")
	_ = a.Steer("s1", "second")

	base := []message.Message{{ID: "u0", Role: message.User}}
	eventCh := make(chan AgentEvent, 8)
	out, injected := a.drainSteeringInto(context.Background(), "s1", base, eventCh)
	if !injected {
		t.Fatal("expected injected=true")
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 messages after injection, got %d", len(out))
	}
	for _, m := range out[1:] {
		if m.Role != message.User {
			t.Fatalf("expected injected messages to be User role, got %v", m.Role)
		}
	}
	// Queue is drained after injection.
	if got := a.PendingSteering("s1"); got != 0 {
		t.Fatalf("expected queue drained, got %d", got)
	}
}

func TestDrainSteeringIntoNoopWhenEmpty(t *testing.T) {
	a := newSteeringTestAgent()
	base := []message.Message{{ID: "u0", Role: message.User}}
	out, injected := a.drainSteeringInto(context.Background(), "s1", base, nil)
	if injected {
		t.Fatal("expected injected=false when queue empty")
	}
	if len(out) != 1 {
		t.Fatalf("expected history unchanged, got %d", len(out))
	}
}
