package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/message"
)

// gatedMessages blocks message.Service.List so a run stays observably alive. The
// first call ignores its context and only returns once release is closed (a run
// that has been cancelled but has not unwound yet); every later call returns as
// soon as its own context is cancelled.
type gatedMessages struct {
	*steeringMockMessages
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func newGatedMessages() *gatedMessages {
	return &gatedMessages{
		steeringMockMessages: newSteeringMockMessages(),
		entered:              make(chan struct{}, 8),
		release:              make(chan struct{}),
	}
}

func (g *gatedMessages) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	g.entered <- struct{}{}
	if g.calls.Add(1) == 1 {
		<-g.release
	} else {
		<-ctx.Done()
	}
	return nil, ctx.Err()
}

func waitEntered(t *testing.T, g *gatedMessages) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("run never reached the blocking provider call")
	}
}

func (a *agent) entryRegistered(key string) bool {
	_, ok := a.activeRequests.Load(key)
	return ok
}

func waitIdle(t *testing.T, a *agent, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.entryRegistered(sessionID) {
		if time.Now().After(deadline) {
			t.Fatal("session never became idle")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func drainEvents(t *testing.T, ch <-chan AgentEvent) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("events channel never closed")
		}
	}
}

// TestCancelledRunIsNotSteerableButStillCountsForIsBusy covers PANDO-US-0069: a
// cancelled run that is still unwinding must not accept steering (the message
// would be lost when the run exits), yet must keep blocking model switches.
func TestCancelledRunIsNotSteerableButStillCountsForIsBusy(t *testing.T) {
	a := newResumeTestAgent(t)
	gm := newGatedMessages()
	a.messages = gm

	first, err := a.Run(context.Background(), "s1", "first")
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	waitEntered(t, gm)
	if !a.IsSessionBusy("s1") {
		t.Fatal("live run not reported busy")
	}

	a.Cancel("s1")
	if a.IsSessionBusy("s1") {
		t.Fatal("cancelled run still reported busy to frontends")
	}
	if err := a.Steer("s1", "hello"); !errors.Is(err, ErrSessionNotBusy) {
		t.Fatalf("Steer on cancelled run = %v, want ErrSessionNotBusy", err)
	}
	if err := a.InjectConclusion("s1", "done"); !errors.Is(err, ErrSessionNotBusy) {
		t.Fatalf("InjectConclusion on cancelled run = %v, want ErrSessionNotBusy", err)
	}
	if !a.IsBusy() {
		t.Fatal("IsBusy must keep counting the unwinding run")
	}

	close(gm.release)
	drainEvents(t, first)
	waitIdle(t, a, "s1")
	if a.IsBusy() {
		t.Fatal("IsBusy still true after the run exited")
	}
}

// TestRunWaitsForCancelledRunToUnwind asserts a Run issued while the cancelled
// run is still unwinding blocks until it exits, then starts (never overlapping),
// and that the new run stays visible and cancellable afterwards.
func TestRunWaitsForCancelledRunToUnwind(t *testing.T) {
	a := newResumeTestAgent(t)
	gm := newGatedMessages()
	a.messages = gm

	first, err := a.Run(context.Background(), "s1", "first")
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	waitEntered(t, gm)
	a.Cancel("s1")

	type result struct {
		ch  <-chan AgentEvent
		err error
	}
	res := make(chan result, 1)
	go func() {
		ch, err := a.Run(context.Background(), "s1", "second")
		res <- result{ch, err}
	}()

	select {
	case <-res:
		t.Fatal("second Run returned while the cancelled run was still unwinding")
	case <-time.After(100 * time.Millisecond):
	}
	// Run 1 has not exited, so run 2 cannot have reached processGeneration.
	if got := gm.calls.Load(); got != 1 {
		t.Fatalf("List calls = %d before run 1 exited, want 1", got)
	}

	close(gm.release)
	drainEvents(t, first)

	var r result
	select {
	case r = <-res:
	case <-time.After(5 * time.Second):
		t.Fatal("second Run never started after the cancelled run exited")
	}
	if r.err != nil {
		t.Fatalf("second Run: %v", r.err)
	}
	waitEntered(t, gm)
	if !a.IsSessionBusy("s1") || !a.IsBusy() {
		t.Fatal("second run is not visible as busy")
	}
	a.Cancel("s1")
	drainEvents(t, r.ch)
	waitIdle(t, a, "s1")
}

// TestRunWaitTimesOutWithErrSessionBusy asserts the wait for a cancelled run is
// bounded and honours ctx cancellation.
func TestRunWaitTimesOutWithErrSessionBusy(t *testing.T) {
	old := cancelledRunWait
	cancelledRunWait = 50 * time.Millisecond
	t.Cleanup(func() { cancelledRunWait = old })

	a := newResumeTestAgent(t)
	gm := newGatedMessages()
	a.messages = gm

	first, err := a.Run(context.Background(), "s1", "first")
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	waitEntered(t, gm)
	a.Cancel("s1")

	if _, err := a.Run(context.Background(), "s1", "second"); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("Run after timeout = %v, want ErrSessionBusy", err)
	}

	cancelledRunWait = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := a.Run(ctx, "s1", "third"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run with expiring ctx = %v, want DeadlineExceeded", err)
	}

	close(gm.release)
	drainEvents(t, first)
	waitIdle(t, a, "s1")
}

// TestFinishingRunKeepsNewerEntry simulates the overlap the old Cancel allowed
// (entry removed, new run registered, old run then cleans up) and asserts the
// old run's cleanup neither removes the newer entry nor its steering.
func TestFinishingRunKeepsNewerEntry(t *testing.T) {
	a := newSteeringTestAgent()
	_, cancel1 := context.WithCancel(context.Background())
	old, _ := a.acquireRun(context.Background(), "s1", cancel1)
	a.activeRequests.Delete("s1") // what the previous Cancel did

	var cancelled atomic.Bool
	_, _ = a.acquireRun(context.Background(), "s1", func() { cancelled.Store(true) })
	if err := a.Steer("s1", "for the new run"); err != nil {
		t.Fatalf("Steer: %v", err)
	}

	a.finishRun("s1", old)

	if !a.IsSessionBusy("s1") || !a.IsBusy() {
		t.Fatal("old run cleanup hid the new run")
	}
	if a.PendingSteering("s1") != 1 {
		t.Fatal("old run cleanup dropped steering queued for the new run")
	}
	a.Cancel("s1")
	if !cancelled.Load() {
		t.Fatal("Cancel could not reach the new run")
	}
}

// TestSummarizeCancelKeepsNewerEntry asserts the same guarantee for the
// "-summarize" entry: a second summarize issued while the cancelled one unwinds
// waits for it, and the first summarize's cleanup never removes the second's
// entry.
func TestSummarizeCancelKeepsNewerEntry(t *testing.T) {
	a := newResumeTestAgent(t)
	gm := newGatedMessages()
	a.messages = gm

	first, err := a.SummarizeStream(context.Background(), "s1")
	if err != nil {
		t.Fatalf("first SummarizeStream: %v", err)
	}
	waitEntered(t, gm)
	a.Cancel("s1")
	if !a.entryRegistered("s1-summarize") {
		t.Fatal("cancelled summarize entry removed before its goroutine exited")
	}

	type result struct {
		ch  <-chan AgentEvent
		err error
	}
	res := make(chan result, 1)
	go func() {
		ch, err := a.SummarizeStream(context.Background(), "s1")
		res <- result{ch, err}
	}()
	select {
	case <-res:
		t.Fatal("second SummarizeStream returned while the cancelled one was unwinding")
	case <-time.After(100 * time.Millisecond):
	}

	close(gm.release)
	drainEvents(t, first)
	var r result
	select {
	case r = <-res:
	case <-time.After(5 * time.Second):
		t.Fatal("second SummarizeStream never started")
	}
	if r.err != nil {
		t.Fatalf("second SummarizeStream: %v", r.err)
	}
	waitEntered(t, gm)
	if !a.entryRegistered("s1-summarize") {
		t.Fatal("first summarize cleanup removed the second summarize entry")
	}
	a.Cancel("s1")
	drainEvents(t, r.ch)
	waitIdle(t, a, "s1-summarize")
}
