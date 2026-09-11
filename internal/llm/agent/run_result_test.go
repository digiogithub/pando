package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/message"
)

func TestCollectRunResultUsesLastEventBeforeClose(t *testing.T) {
	ch := make(chan AgentEvent, 8)
	ch <- AgentEvent{Type: AgentEventTypeSystemMessage, SystemMessage: "starting"}
	ch <- AgentEvent{Type: AgentEventTypeThinkingDelta, Delta: "thinking"}
	ch <- AgentEvent{Type: AgentEventTypeContentDelta, Delta: "hello"}
	ch <- AgentEvent{Type: AgentEventTypeTokenUsage, TokenUsage: &TokenUsageInfo{PromptTokens: 10}}
	final := AgentEvent{Type: AgentEventTypeResponse, Done: true, Message: message.Message{Role: message.Assistant}}
	ch <- final
	close(ch)

	got, timedOut := CollectRunResult(context.Background(), ch, nil)
	if timedOut {
		t.Fatal("timedOut = true, want false")
	}
	if got.Type != AgentEventTypeResponse || !got.Done {
		t.Fatalf("result = %+v, want the final Response event", got)
	}
}

func TestCollectRunResultIgnoresNonTerminalError(t *testing.T) {
	ch := make(chan AgentEvent, 8)
	ch <- AgentEvent{Type: AgentEventTypeError, Error: errCompactionExample}
	final := AgentEvent{Type: AgentEventTypeResponse, Done: true, Message: message.Message{Role: message.Assistant}}
	ch <- final
	close(ch)

	got, timedOut := CollectRunResult(context.Background(), ch, nil)
	if timedOut {
		t.Fatal("timedOut = true, want false")
	}
	if got.Error != nil {
		t.Fatalf("result.Error = %v, want nil (final event should win)", got.Error)
	}
	if got.Type != AgentEventTypeResponse {
		t.Fatalf("result.Type = %v, want Response", got.Type)
	}
}

func TestCollectRunResultReturnsFinalError(t *testing.T) {
	ch := make(chan AgentEvent, 4)
	ch <- AgentEvent{Type: AgentEventTypeSystemMessage, SystemMessage: "starting"}
	final := AgentEvent{Type: AgentEventTypeError, Error: errCompactionExample}
	ch <- final
	close(ch)

	got, timedOut := CollectRunResult(context.Background(), ch, nil)
	if timedOut {
		t.Fatal("timedOut = true, want false")
	}
	if got.Error != errCompactionExample {
		t.Fatalf("result.Error = %v, want %v", got.Error, errCompactionExample)
	}
}

func TestCollectRunResultClosedWithoutEvents(t *testing.T) {
	ch := make(chan AgentEvent)
	close(ch)

	got, timedOut := CollectRunResult(context.Background(), ch, nil)
	if timedOut {
		t.Fatal("timedOut = true, want false")
	}
	if got.Type != "" {
		t.Fatalf("result = %+v, want the zero event", got)
	}
}

func TestCollectRunResultTimeoutCancelsAndWaitsForClose(t *testing.T) {
	ch := make(chan AgentEvent)
	ctx, cancel := context.WithCancel(context.Background())

	cancelCalled := make(chan struct{}, 1)
	cancelRun := func() {
		cancelCalled <- struct{}{}
		// Simulate the agent's own cancellation path: it keeps writing for a
		// little while (its final DB update) before it actually sends the
		// terminal event and closes the channel.
		go func() {
			time.Sleep(20 * time.Millisecond)
			ch <- AgentEvent{Type: AgentEventTypeError, Error: ErrRequestCancelled}
			close(ch)
		}()
	}

	cancel() // ctx already done, as if the run timed out

	done := make(chan struct{})
	var got AgentEvent
	var timedOut bool
	go func() {
		got, timedOut = CollectRunResult(ctx, ch, cancelRun)
		close(done)
	}()

	select {
	case <-cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("cancelRun was never called")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CollectRunResult did not return after the channel closed")
	}

	if !timedOut {
		t.Fatal("timedOut = false, want true")
	}
	if got.Error != ErrRequestCancelled {
		t.Fatalf("result.Error = %v, want %v", got.Error, ErrRequestCancelled)
	}
}

func TestCollectRunResultGivesUpAfterGraceTimeout(t *testing.T) {
	origGrace := runDrainGrace
	runDrainGrace = 10 * time.Millisecond
	defer func() { runDrainGrace = origGrace }()

	ch := make(chan AgentEvent) // never written to, never closed: a stuck agent
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, timedOut := CollectRunResult(ctx, ch, func() {})
	if !timedOut {
		t.Fatal("timedOut = false, want true")
	}
	if got.Type != "" {
		t.Fatalf("result = %+v, want the zero event", got)
	}
}

var errCompactionExample = &testError{"compaction failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
