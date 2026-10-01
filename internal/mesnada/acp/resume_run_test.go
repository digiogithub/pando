package acp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// newResumeTestAgent returns an agent whose sessions have a client connection
// that records every notification into the returned buffer.
func newResumeTestAgent(t *testing.T) (*PandoACPAgent, *mockAgentService, *bytes.Buffer, *ACPServerSession) {
	t.Helper()
	svc := &mockAgentService{}
	sessions := newMockSessionService()
	agent := NewPandoACPAgent("1.0.0-test", "/tmp", log.New(io.Discard, "", 0), svc, sessions, nil)
	var updates bytes.Buffer
	agent.conn = acpsdk.NewAgentSideConnection(NewSimpleACPAgent("1.0.0-test", log.New(io.Discard, "", 0)), &updates, bytes.NewReader(nil))

	resp, err := agent.NewSession(context.Background(), acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	return agent, svc, &updates, acpSession
}

func waitResumedRuns(t *testing.T, agent *PandoACPAgent) {
	t.Helper()
	done := make(chan struct{})
	go func() { agent.resumeWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("resumed run did not finish")
	}
}

func eventsOf(evs ...AgentEvent) <-chan AgentEvent {
	ch := make(chan AgentEvent, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch
}

func TestTakeResumedRunDeclinesUnknownSession(t *testing.T) {
	agent, _, updates, _ := newResumeTestAgent(t)
	called := false
	taken, err := agent.TakeResumedRun("some-other-session", func(context.Context) (<-chan AgentEvent, error) {
		called = true
		return eventsOf(), nil
	})
	if taken || err != nil || called {
		t.Fatalf("taken=%v err=%v startCalled=%v, want false/nil/false", taken, err, called)
	}
	if updates.Len() != 0 {
		t.Fatalf("declined run wrote updates: %s", updates.String())
	}
}

func TestTakeResumedRunDeclinesSessionWithoutClient(t *testing.T) {
	agent := newTestPandoAgent() // no connection: sessions have no client to stream to
	resp, err := agent.NewSession(context.Background(), acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	taken, err := agent.TakeResumedRun(string(resp.SessionId), func(context.Context) (<-chan AgentEvent, error) {
		t.Fatal("start must not be called without a client")
		return nil, nil
	})
	if taken || err != nil {
		t.Fatalf("taken=%v err=%v, want false/nil", taken, err)
	}
}

func TestTakeResumedRunMatchesPandoSessionID(t *testing.T) {
	agent, _, _, sess := newResumeTestAgent(t)
	// The ACP id and the Pando id differ for a session registered under another key.
	other := NewACPServerSession("acp-id", "/tmp", agent.conn, "pando-id")
	agent.sessionsMu.Lock()
	agent.sessions["acp-id"] = other
	agent.sessionsMu.Unlock()

	if got := agent.sessionByPandoID("pando-id"); got != other {
		t.Fatalf("lookup by pando id = %v, want the bound session", got)
	}
	if got := agent.sessionByPandoID("acp-id"); got != nil {
		t.Fatalf("lookup by ACP id must not match, got %v", got)
	}
	if got := agent.sessionByPandoID(sess.PandoSessionID()); got != sess {
		t.Fatal("session created by session/new not found")
	}
}

func TestTakeResumedRunForwardsEventsAndPostRunUpdates(t *testing.T) {
	agent, svc, updates, sess := newResumeTestAgent(t)
	svc.lastRunMessages = []string{"status after resume"}

	taken, err := agent.TakeResumedRun(sess.PandoSessionID(), func(context.Context) (<-chan AgentEvent, error) {
		return eventsOf(
			AgentEvent{Type: AgentEventTypeSystemMessage, SystemMessage: "Resuming - a delegated task reported its result.", MessageID: "notice-1"},
			AgentEvent{Type: AgentEventTypeContentDelta, Delta: "all done", MessageID: "msg-resumed"},
			AgentEvent{Type: AgentEventTypeResponse, Message: message.Message{ID: "msg-resumed", Role: message.Assistant}},
		), nil
	})
	if !taken || err != nil {
		t.Fatalf("taken=%v err=%v, want true/nil", taken, err)
	}
	waitResumedRuns(t, agent)

	recs := decodeSessionUpdateRecords(t, updates.String())
	var marker, delta *acpUpdateRecord
	kinds := map[string]bool{}
	for i := range recs {
		kinds[recs[i].Kind] = true
		if recs[i].Kind == "agent_message_chunk" && strings.Contains(recs[i].Text, "Resuming") {
			marker = &recs[i]
		}
		if recs[i].Kind == "agent_message_chunk" && recs[i].Text == "all done" {
			delta = &recs[i]
		}
	}
	if marker == nil || marker.MessageID != "notice-1" {
		t.Fatalf("marker not forwarded with its own messageId: %+v", recs)
	}
	if delta == nil || delta.MessageID != "msg-resumed" {
		t.Fatalf("content delta not forwarded with its messageId: %+v", recs)
	}
	if marker.MessageID == delta.MessageID {
		t.Fatal("the marker must not share a messageId with the assistant message")
	}
	if !kinds["session_info_update"] {
		t.Fatalf("post-run session_info_update (run status meta) missing: %+v", recs)
	}
}

func TestTakeResumedRunReportsStartError(t *testing.T) {
	agent, _, updates, sess := newResumeTestAgent(t)
	busy := errors.New("session busy")
	taken, err := agent.TakeResumedRun(sess.PandoSessionID(), func(context.Context) (<-chan AgentEvent, error) {
		return nil, busy
	})
	if !taken || !errors.Is(err, busy) {
		t.Fatalf("taken=%v err=%v, want true/%v", taken, err, busy)
	}
	waitResumedRuns(t, agent)
	if updates.Len() != 0 {
		t.Fatalf("failed start wrote updates: %s", updates.String())
	}
}

func TestTakeResumedRunErrorEventIsShown(t *testing.T) {
	agent, _, updates, sess := newResumeTestAgent(t)
	_, _ = agent.TakeResumedRun(sess.PandoSessionID(), func(context.Context) (<-chan AgentEvent, error) {
		return eventsOf(AgentEvent{Type: AgentEventTypeError, Error: errors.New("provider exploded")}), nil
	})
	waitResumedRuns(t, agent)
	if !strings.Contains(updates.String(), "provider exploded") {
		t.Fatalf("run failure not surfaced to the client: %s", updates.String())
	}
}

// session/cancel with no prompt in flight must stop a resumed run: it cancels
// the run context and reaches the agent service.
func TestCancelStopsResumedRun(t *testing.T) {
	agent, svc, _, sess := newResumeTestAgent(t)
	started := make(chan struct{})
	taken, err := agent.TakeResumedRun(sess.PandoSessionID(), func(ctx context.Context) (<-chan AgentEvent, error) {
		ch := make(chan AgentEvent)
		go func() {
			defer close(ch)
			close(started)
			<-ctx.Done() // a real run ends when its context is cancelled
		}()
		return ch, nil
	})
	if !taken || err != nil {
		t.Fatalf("taken=%v err=%v", taken, err)
	}
	<-started

	if err := agent.Cancel(context.Background(), acpsdk.CancelNotification{SessionId: sess.ID}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waitResumedRuns(t, agent) // returns only once the forwarding goroutine ended
	if !svc.cancelCalled {
		t.Fatal("cancel did not reach the agent service")
	}
}

// A prompt that arrives while a resumed run is in flight is queued as steering,
// never started as a second run.
func TestPromptDuringResumedRunIsSteering(t *testing.T) {
	agent, svc, _, sess := newResumeTestAgent(t)
	svc.busy = true // the agent reports the resumed run as active
	release := make(chan struct{})
	taken, _ := agent.TakeResumedRun(sess.PandoSessionID(), func(ctx context.Context) (<-chan AgentEvent, error) {
		ch := make(chan AgentEvent)
		go func() { defer close(ch); <-release }()
		return ch, nil
	})
	if !taken {
		t.Fatal("run not taken")
	}

	_, err := agent.Prompt(context.Background(), acpsdk.PromptRequest{
		SessionId: sess.ID,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("also check the tests")},
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	close(release)
	waitResumedRuns(t, agent)

	if len(svc.steerCalls) != 1 || svc.steerCalls[0] != "also check the tests" {
		t.Fatalf("steerCalls = %v, want the prompt queued as steering", svc.steerCalls)
	}
	if svc.runCalled {
		t.Fatal("a prompt during a resumed run must not start a second run")
	}
}
