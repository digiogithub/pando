package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/userinput"
)

// resumeFakeAgent is an agent.Service whose busy state, broker and cancellation
// the tests drive directly. The embedded nil interface makes any method the
// tests do not exercise panic loudly.
type resumeFakeAgent struct {
	agent.Service
	mu        sync.Mutex
	busy      map[string]bool
	cancelled []string
	broker    chan pubsub.Event[agent.AgentEvent]
}

func newResumeFakeAgent() *resumeFakeAgent {
	return &resumeFakeAgent{busy: map[string]bool{}, broker: make(chan pubsub.Event[agent.AgentEvent], 16)}
}

func (f *resumeFakeAgent) setBusy(id string, v bool) {
	f.mu.Lock()
	f.busy[id] = v
	f.mu.Unlock()
}

func (f *resumeFakeAgent) IsSessionBusy(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy[id]
}

func (f *resumeFakeAgent) Cancel(id string) {
	f.mu.Lock()
	f.cancelled = append(f.cancelled, id)
	f.mu.Unlock()
}

func (f *resumeFakeAgent) Subscribe(context.Context) <-chan pubsub.Event[agent.AgentEvent] {
	return f.broker
}

func newResumeTestServer(ag agent.Service) *Server {
	s := &Server{
		app: &app.App{
			CoderAgent:     ag,
			ResumeHandlers: agent.NewResumeRegistry(),
			Permissions:    permission.NewPermissionService(),
			UserInput:      userinput.NewService(),
		},
		bgRunner: NewBackgroundSessionManager(),
	}
	s.registerResumeHandler()
	return s
}

// resumeStart returns a ResumeStart whose run emits Resurrected first and then
// whatever the test pushes, until the test closes the channel or the run ctx is
// cancelled.
func resumeStart(ch chan agent.AgentEvent, ctxOut *context.Context) agent.ResumeStart {
	return func(ctx context.Context) (<-chan agent.AgentEvent, error) {
		*ctxOut = ctx
		ch <- agent.AgentEvent{Type: agent.AgentEventTypeResurrected, SessionID: "s1"}
		return ch, nil
	}
}

func TestResumedRunIsSubmittedThroughBackgroundRunner(t *testing.T) {
	ag := newResumeFakeAgent()
	s := newResumeTestServer(ag)

	runCh := make(chan agent.AgentEvent, 8)
	var runCtx context.Context
	taken, err := s.app.ResumeHandlers.Offer("s1", resumeStart(runCh, &runCtx))
	if !taken || err != nil {
		t.Fatalf("taken=%v err=%v, want the API server to take the run", taken, err)
	}

	// The run is visible to the busy flag even though the agent knows nothing.
	if !s.sessionRunning("s1") || !s.bgRunner.IsBusy("s1") {
		t.Fatal("session must be reported running during a resumed run")
	}

	// A late subscriber replays the Resurrected event, then goes live.
	sub, unsub, ok := s.bgRunner.Subscribe("s1")
	if !ok {
		t.Fatal("bgRunner has no session for the resumed run")
	}
	defer unsub()
	if ev := recvEvent(t, sub); ev.Type != agent.AgentEventTypeResurrected {
		t.Fatalf("first replayed event = %q, want resurrected", ev.Type)
	}
	runCh <- agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, SessionID: "s1", Delta: "hi"}
	if ev := recvEvent(t, sub); ev.Delta != "hi" {
		t.Fatalf("live event = %+v", ev)
	}

	close(runCh)
	if _, open := <-sub; open {
		t.Fatal("stream must close when the run ends")
	}
	waitUntil(t, func() bool { return !s.sessionRunning("s1") })
}

func TestResumedRunBusyOnlyFromAgentIsReportedRunning(t *testing.T) {
	ag := newResumeFakeAgent()
	s := newResumeTestServer(ag)

	if s.sessionRunning("s1") {
		t.Fatal("idle session reported running")
	}
	ag.setBusy("s1", true)
	if s.bgRunner.IsBusy("s1") {
		t.Fatal("test setup: bgRunner must not know the run")
	}
	if !s.sessionRunning("s1") {
		t.Fatal("a run held only by the agent must be reported running")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/s1/pending", nil)
	req.SetPathValue("id", "s1")
	rec := httptest.NewRecorder()
	s.handleSessionPending(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["running"] != true {
		t.Fatalf("/pending running = %v, want true", body["running"])
	}
}

func TestResumedRunBusyRaceReportsErrSessionBusy(t *testing.T) {
	ag := newResumeFakeAgent()
	s := newResumeTestServer(ag)

	first := make(chan agent.AgentEvent, 1)
	var ctx1 context.Context
	if _, err := s.app.ResumeHandlers.Offer("s1", resumeStart(first, &ctx1)); err != nil {
		t.Fatalf("first resume: %v", err)
	}
	// The agent is busy too (a real second run), so the refusal is genuine and
	// must reach the supervisor for its injection fallback.
	ag.setBusy("s1", true)
	var ctx2 context.Context
	taken, err := s.app.ResumeHandlers.Offer("s1", resumeStart(make(chan agent.AgentEvent, 1), &ctx2))
	if !taken || err != agent.ErrSessionBusy {
		t.Fatalf("taken=%v err=%v, want taken with ErrSessionBusy", taken, err)
	}
	close(first)
}

func TestCancelStopsResumedRun(t *testing.T) {
	ag := newResumeFakeAgent()
	s := newResumeTestServer(ag)

	runCh := make(chan agent.AgentEvent, 8)
	var runCtx context.Context
	if _, err := s.app.ResumeHandlers.Offer("s1", resumeStart(runCh, &runCtx)); err != nil {
		t.Fatalf("resume: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/s1/cancel", nil)
	req.SetPathValue("id", "s1")
	rec := httptest.NewRecorder()
	s.handleSessionCancel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not cancel the resumed run's context")
	}
	if len(ag.cancelled) != 1 || ag.cancelled[0] != "s1" {
		t.Fatalf("agent.Cancel calls = %v", ag.cancelled)
	}
	close(runCh)
}

func TestNoHandlerRegisteredLeavesRegistryEmpty(t *testing.T) {
	// A server whose app has no registry (or a TUI/CLI app with nothing registered)
	// must not panic and must not take runs.
	s := &Server{app: &app.App{}, bgRunner: NewBackgroundSessionManager()}
	s.registerResumeHandler()

	reg := agent.NewResumeRegistry()
	if taken, err := reg.Offer("s1", nil); taken || err != nil {
		t.Fatalf("empty registry took a run: taken=%v err=%v", taken, err)
	}
}

func TestServerShutdownUnregistersResumeHandler(t *testing.T) {
	s := newResumeTestServer(newResumeFakeAgent())
	s.unregisterResume()
	if taken, _ := s.app.ResumeHandlers.Offer("s1", nil); taken {
		t.Fatal("unregistered server must not take runs")
	}
}

func TestBrokerRunEventsForwardsSessionEventsUntilIdle(t *testing.T) {
	ag := newResumeFakeAgent()
	ag.setBusy("s1", true)
	s := newResumeTestServer(ag)

	ch, stop := s.brokerRunEvents(context.Background(), "s1")
	defer stop()

	ag.broker <- pubsub.Event[agent.AgentEvent]{Payload: agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, SessionID: "other", Delta: "no"}}
	ag.broker <- pubsub.Event[agent.AgentEvent]{Payload: agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, SessionID: "s1", Delta: "yes"}}
	if ev := recvEvent(t, ch); ev.Delta != "yes" {
		t.Fatalf("got %+v, want the s1 event only", ev)
	}

	ag.setBusy("s1", false)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("broker stream did not end after the agent went idle")
		}
	}
}

func TestDispatchSSEEventMapsDelegationEvents(t *testing.T) {
	s := &Server{}
	cases := map[agent.AgentEventType]string{
		agent.AgentEventTypeResurrected:        "event: resurrected",
		agent.AgentEventTypeConclusionQueued:   "event: conclusion_queued",
		agent.AgentEventTypeConclusionInjected: "event: conclusion_injected",
	}
	for typ, want := range cases {
		rec := httptest.NewRecorder()
		var mu sync.Mutex
		s.dispatchSSEEvent(rec, rec, &mu, map[string]bool{}, map[string]string{}, "", agent.AgentEvent{
			Type: typ, SessionID: "s1", SystemMessage: "msg",
		})
		out := rec.Body.String()
		if !strings.Contains(out, want) || !strings.Contains(out, `"session_id":"s1"`) || !strings.Contains(out, `"message":"msg"`) {
			t.Errorf("%s: output %q does not contain %q with session and message", typ, out, want)
		}
	}
}

func recvEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed early")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	var zero T
	return zero
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
