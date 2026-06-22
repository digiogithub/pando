package agent

import (
	"context"
	"testing"
	"time"

	llmmodels "github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

// resumeStubProvider is a minimal provider.Provider so runInternal passes its
// a.provider == nil guard and returns a run channel. The async run goroutine may
// fail (the test agent has no full session/message wiring); that is recovered and
// drained, and is irrelevant to the synchronous Resume semantics under test.
type resumeStubProvider struct{}

func (resumeStubProvider) SendMessages(ctx context.Context, messages []message.Message, t []tools.BaseTool) (*provider.ProviderResponse, error) {
	return &provider.ProviderResponse{}, nil
}

func (resumeStubProvider) StreamResponse(ctx context.Context, messages []message.Message, t []tools.BaseTool) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent)
	close(ch)
	return ch
}

func (resumeStubProvider) Model() llmmodels.Model {
	return llmmodels.Model{ID: "stub", SupportsAttachments: false}
}

// resumeStubSessions is a no-op session.Service good enough for processGeneration
// to fail fast/cleanly inside the recovered run goroutine.
type resumeStubSessions struct {
	*pubsub.Broker[session.Session]
}

func newResumeStubSessions() *resumeStubSessions {
	return &resumeStubSessions{Broker: pubsub.NewBroker[session.Session]()}
}

func (s *resumeStubSessions) Create(ctx context.Context, title string) (session.Session, error) {
	return session.Session{}, nil
}
func (s *resumeStubSessions) CreateTitleSession(ctx context.Context, parentSessionID string) (session.Session, error) {
	return session.Session{}, nil
}
func (s *resumeStubSessions) CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (session.Session, error) {
	return session.Session{}, nil
}
func (s *resumeStubSessions) GetACPSessionState(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}
func (s *resumeStubSessions) Get(ctx context.Context, id string) (session.Session, error) {
	return session.Session{ID: id}, nil
}
func (s *resumeStubSessions) List(ctx context.Context) ([]session.Session, error) { return nil, nil }
func (s *resumeStubSessions) SaveACPSessionState(ctx context.Context, sessionID, state string) error {
	return nil
}
func (s *resumeStubSessions) Save(ctx context.Context, sess session.Session) (session.Session, error) {
	return sess, nil
}
func (s *resumeStubSessions) Delete(ctx context.Context, id string) error     { return nil }
func (s *resumeStubSessions) EndSession(ctx context.Context, id string) error { return nil }

// newResumeTestAgent extends the steering test agent with a stub provider and
// sessions so Resume can start (and the run goroutine can fail-and-recover).
func newResumeTestAgent() *agent {
	a := newSteeringTestAgent()
	a.provider = resumeStubProvider{}
	a.sessions = newResumeStubSessions()
	a.resurrectCount = make(map[string]int)
	return a
}

func TestResumeRejectsEmptyContent(t *testing.T) {
	a := newResumeTestAgent()
	if err := a.Resume(context.Background(), "s1", "   "); err == nil {
		t.Fatal("expected error for empty content")
	}
	if a.ResurrectionCount("s1") != 0 {
		t.Fatalf("expected count 0, got %d", a.ResurrectionCount("s1"))
	}
}

func TestResumeRejectsWhenBusy(t *testing.T) {
	a := newResumeTestAgent()
	a.markBusy("s1")
	if err := a.Resume(context.Background(), "s1", "resume content"); err != ErrSessionBusy {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	if a.ResurrectionCount("s1") != 0 {
		t.Fatalf("expected count 0 when busy, got %d", a.ResurrectionCount("s1"))
	}
}

func TestResumeIncrementsCountAndEmitsResurrectedEvent(t *testing.T) {
	a := newResumeTestAgent()
	sub := a.Subscribe(context.Background())

	if err := a.Resume(context.Background(), "s1", "you are resuming"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ResurrectionCount("s1") != 1 {
		t.Fatalf("expected count 1 after one Resume, got %d", a.ResurrectionCount("s1"))
	}

	// The Resurrected framing event must be published.
	deadline := time.After(time.Second)
	var sawResurrected bool
	for !sawResurrected {
		select {
		case ev := <-sub:
			if ev.Payload.Type == AgentEventTypeResurrected {
				sawResurrected = true
			}
		case <-deadline:
			t.Fatal("expected AgentEventTypeResurrected to be published")
		}
	}
	// Let the recovered run goroutine settle.
	a.Cancel("s1")
}

func TestRunResetsResurrectionCount(t *testing.T) {
	a := newResumeTestAgent()
	// Seed a non-zero count as if the session had been resurrected.
	a.incrementResurrectionCount("s1")
	a.incrementResurrectionCount("s1")
	if a.ResurrectionCount("s1") != 2 {
		t.Fatalf("setup: expected count 2, got %d", a.ResurrectionCount("s1"))
	}

	// A user-initiated Run must reset the counter to 0. Run will start (provider is
	// non-nil); the async goroutine is recovered/drained.
	events, err := a.Run(context.Background(), "s1", "user message")
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
	if a.ResurrectionCount("s1") != 0 {
		t.Fatalf("expected count reset to 0 by Run, got %d", a.ResurrectionCount("s1"))
	}
	// Drain the run channel so the goroutine can exit.
	go func() {
		for range events {
		}
	}()
	a.Cancel("s1")
}

func TestCancelClearsResurrectionCount(t *testing.T) {
	a := newResumeTestAgent()
	a.incrementResurrectionCount("s1")
	if a.ResurrectionCount("s1") != 1 {
		t.Fatalf("setup: expected count 1, got %d", a.ResurrectionCount("s1"))
	}
	a.Cancel("s1")
	if a.ResurrectionCount("s1") != 0 {
		t.Fatalf("expected count cleared by Cancel, got %d", a.ResurrectionCount("s1"))
	}
}
