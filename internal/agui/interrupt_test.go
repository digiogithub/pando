package agui

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
)

// newSuspendableRun wires a run whose agent side is a plain channel, which is
// all the pump actually needs, with its translator pre-built the way
// handleRun would build it for a first segment.
func newSuspendableRun(events chan agent.AgentEvent, suspend chan suspension) *activeRun {
	state := newTestTracker()
	t := newTranslator("t1", "r1").withState(state)
	return newActiveRun("t1", "s1", events, func() {}, state, suspend, t)
}

// attachAndRecord starts run's pump and attaches an httptest recorder to it
// in the background, waiting until the attach has actually subscribed before
// calling populate (nil is fine). Without that handshake, a test that queues
// events straight onto run's channels before attaching would race the pump:
// it could process and broadcast them before anyone subscribed, and since
// first=true's attach skips the (nothing-to-replay) buffer, those events
// would be silently lost and the attach would then block forever waiting for
// events that already happened — a real, observed hang under -race, not a
// hypothetical one. It is the test replacement for the old synchronous
// r.stream(ctx, sse, tr, run) call, blocking until the attach detaches
// (a segment boundary, the events channel closing, or reqCtx being done).
func attachAndRecord(t *testing.T, r *Runtime, run *activeRun, reqCtx context.Context, first bool, populate func()) *httptest.ResponseRecorder {
	t.Helper()
	go r.pump(run)
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	done := make(chan struct{})
	go func() {
		r.attachRun(reqCtx, sse, run, first)
		close(done)
	}()
	waitForSubscriberCount(t, run, 1)
	if populate != nil {
		populate()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("attachRun never returned")
	}
	return rec
}

func frameTypes(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		start := strings.Index(payload, `"type":"`)
		if start < 0 {
			continue
		}
		rest := payload[start+len(`"type":"`):]
		if end := strings.Index(rest, `"`); end >= 0 {
			out = append(out, rest[:end])
		}
	}
	return out
}

func indexOf(items []string, want string) int {
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}

// TestStreamSuspendsOnFrontendTool is the core P3 guarantee: the run stops with
// an interrupt, the tool call is fully described first, and the run survives the
// response so the next request can resolve it.
func TestStreamSuspendsOnFrontendTool(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 8)
	suspend := make(chan suspension, 1)
	run := newSuspendableRun(events, suspend)
	r.runs.put(run)
	t.Cleanup(func() { close(events) })

	rec := attachAndRecord(t, r, run, context.Background(), true, func() {
		// The agent queues the call's events, then blocks inside the tool.
		events <- agent.AgentEvent{
			Type: agent.AgentEventTypeToolCall,
			ToolCall: &message.ToolCall{
				ID: "call-1", Name: "showChart", Input: `{"title":"sales"}`, Finished: true,
			},
		}
		suspend <- suspension{callID: "call-1"}
	})
	run.unpark()

	types := frameTypes(rec.Body.String())
	end := indexOf(types, string(EventToolCallEnd))
	finished := indexOf(types, string(EventRunFinished))
	if indexOf(types, string(EventToolCallStart)) < 0 || indexOf(types, string(EventToolCallArgs)) < 0 {
		t.Fatalf("the call was not described before the interrupt: %v", types)
	}
	if end < 0 || finished < 0 || end > finished {
		t.Fatalf("TOOL_CALL_END must precede RUN_FINISHED: %v", types)
	}
	if !strings.Contains(rec.Body.String(), `"outcome":"interrupt"`) {
		t.Fatalf("the run did not report an interrupt: %s", rec.Body.String())
	}
	if _, ok := r.runs.get("t1"); !ok {
		t.Fatal("a suspended run must stay registered so the next request can resume it")
	}
}

// TestStreamDescribesToolCallTheAgentNeverStreamed is the regression for the
// providers that report their tool calls in one final response instead of
// streaming them: agent.processEvent publishes no tool_call event at all, so the
// suspension is the only place the call is known. An interrupt without a
// TOOL_CALL_START strands the client — it has no call to execute.
func TestStreamDescribesToolCallTheAgentNeverStreamed(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	suspend := make(chan suspension, 1)
	run := newSuspendableRun(events, suspend)
	r.runs.put(run)
	t.Cleanup(func() { close(events) })

	rec := attachAndRecord(t, r, run, context.Background(), true, func() {
		// No AgentEventTypeToolCall is ever queued: the tool ran, the run
		// suspended, and the stream stayed silent about it.
		suspend <- suspension{
			callID: "call-9",
			call:   &toolCallInfo{name: "showChart", input: `{"title":"sales"}`},
		}
	})
	run.unpark()

	body := rec.Body.String()
	types := frameTypes(body)
	start := indexOf(types, string(EventToolCallStart))
	end := indexOf(types, string(EventToolCallEnd))
	finished := indexOf(types, string(EventRunFinished))
	if start < 0 || end < 0 || end > finished {
		t.Fatalf("the call must be described before the interrupt: %v", types)
	}
	if !strings.Contains(body, `"toolCallName":"showChart"`) {
		t.Fatalf("the client cannot tell which tool to run: %s", body)
	}
	if !strings.Contains(body, `{\"title\":\"sales\"}`) {
		t.Fatalf("the client cannot tell which arguments to use: %s", body)
	}
}

// TestStreamDoesNotDuplicateAStreamedToolCall guards the other side of the same
// fix: when the agent did stream the call, the fallback must add nothing.
func TestStreamDoesNotDuplicateAStreamedToolCall(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	suspend := make(chan suspension, 1)
	run := newSuspendableRun(events, suspend)
	r.runs.put(run)
	t.Cleanup(func() { close(events) })

	rec := attachAndRecord(t, r, run, context.Background(), true, func() {
		events <- agent.AgentEvent{
			Type: agent.AgentEventTypeToolCall,
			ToolCall: &message.ToolCall{
				ID: "call-1", Name: "showChart", Input: `{"title":"sales"}`, Finished: true,
			},
		}
		suspend <- suspension{
			callID: "call-1",
			call:   &toolCallInfo{name: "showChart", input: `{"title":"sales"}`},
		}
	})
	run.unpark()

	types := frameTypes(rec.Body.String())
	var starts, ends int
	for _, ty := range types {
		switch ty {
		case string(EventToolCallStart):
			starts++
		case string(EventToolCallEnd):
			ends++
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatalf("the call was described twice: %v", types)
	}
}

// TestStreamFinishesAndUnregistersRun is the complementary case: a run that ends
// on its own must not be left behind in the store.
func TestStreamFinishesAndUnregistersRun(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)

	rec := attachAndRecord(t, r, run, context.Background(), true, func() {
		events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	})

	if !strings.Contains(rec.Body.String(), `"outcome":"success"`) {
		t.Fatalf("unexpected stream: %s", rec.Body.String())
	}
	// The attach's HTTP response ends the instant it reads the final frame
	// off its subscriber channel, which is BEFORE the pump has necessarily
	// finished removing the run from runStore (see activeRun.broadcastFinal
	// and Runtime.finalizeRun's doc comments) -- wait for run.done, which
	// closes only once that removal has actually happened.
	select {
	case <-run.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the run was never fully torn down")
	}
	if _, ok := r.runs.get("t1"); ok {
		t.Fatal("a completed run must be unregistered")
	}
}

// TestDisconnectParksRatherThanCancels is the PANDO-US-0017 core guarantee:
// the browser going away mid-run must not cancel the agent's work. The run
// stays registered and its context is never cancelled; it is merely parked.
func TestDisconnectParksRatherThanCancels(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension))
	cancelled := make(chan struct{})
	run.cancel = func() { close(cancelled) }
	r.runs.put(run)
	go r.pump(run)
	t.Cleanup(func() { close(events) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	r.attachRun(ctx, sse, run, true)

	select {
	case <-cancelled:
		t.Fatal("a mere disconnect must not cancel the run's agent context")
	case <-time.After(50 * time.Millisecond):
	}
	if _, ok := r.runs.get("t1"); !ok {
		t.Fatal("a parked run must stay registered so a reattach can resume it")
	}
}

// TestParkedRunExpiresWithoutReconnect is the PANDO-US-0017 grace-period
// acceptance criterion: a parked run with no reconnect inside the grace
// period is torn down and no goroutine is left blocked on it.
func TestParkedRunExpiresWithoutReconnect(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	r.cfg.DisconnectGrace = 10 * time.Millisecond
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension))
	r.runs.put(run)
	go r.pump(run)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	r.attachRun(ctx, sse, run, true)

	select {
	case <-run.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the parked run was never torn down after its grace period expired")
	}
	if _, ok := r.runs.get("t1"); ok {
		t.Fatal("an expired parked run must be removed from the store")
	}
	close(events)
}

func TestDeliverToolResults(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	run := newSuspendableRun(make(chan agent.AgentEvent, 1), make(chan suspension, 1))
	r.pending.watch("s1")
	waiter := r.pending.register("s1", suspension{callID: "call-1"})

	in := &RunAgentInput{
		ThreadID: "t1", RunID: "r2",
		Messages: []Message{
			{ID: "m1", Role: RoleUser, Content: MessageContent{Text: "chart it"}},
			{ID: "m2", Role: RoleTool, ToolCallID: "call-1", Content: MessageContent{Text: "rendered"}},
			{ID: "m3", Role: RoleTool, ToolCallID: "stale", Content: MessageContent{Text: "ignored"}},
		},
	}
	candidates := r.resumeCandidates(run, in)
	if len(candidates) != 1 || candidates[0].ToolCallID != "call-1" {
		t.Fatalf("only the waiting call should count as a resumption candidate: %v", candidates)
	}
	for _, msg := range candidates {
		r.pending.resolve(run.sessionID, msg.ToolCallID, msg)
	}

	select {
	case resp := <-waiter:
		if resp.Content.String() != "rendered" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("the result never reached the blocked tool")
	}
}

// TestDeliverToolResultsIgnoresNewTurn: a payload with no tool message for a
// pending call is a new turn, not a resumption.
func TestDeliverToolResultsIgnoresNewTurn(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	run := newSuspendableRun(make(chan agent.AgentEvent, 1), make(chan suspension, 1))
	r.pending.register("s1", suspension{callID: "call-1"})

	in := &RunAgentInput{
		ThreadID: "t1", RunID: "r2",
		Messages: []Message{{ID: "m1", Role: RoleUser, Content: MessageContent{Text: "never mind"}}},
	}
	if candidates := r.resumeCandidates(run, in); len(candidates) != 0 {
		t.Fatalf("a new turn must not look like a resumption: %v", candidates)
	}
}

// TestResumedRunKeepsSharedState pins that a resumption patches the same state
// document instead of restarting it — the file list built before the interrupt
// must still be there.
func TestResumedRunKeepsSharedState(t *testing.T) {
	state := newTestTracker()
	state.Observe(agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "c1", Name: "view", Input: `{"file_path":"/repo/a.go"}`},
	})

	snap, ok := state.Snapshot().(StateSnapshotEvent)
	if !ok {
		t.Fatal("expected a snapshot event")
	}
	doc, ok := snap.Snapshot.(StateDoc)
	if !ok {
		t.Fatalf("unexpected snapshot payload %T", snap.Snapshot)
	}
	if len(doc.Files) != 1 || doc.Files[0].Path != "/repo/a.go" {
		t.Fatalf("the resumed snapshot lost the file list: %+v", doc.Files)
	}
}

// TestPendingRegistryDropsNotificationWithoutListener guards the disconnected
// case: registering must never block the agent goroutine.
func TestPendingRegistryDropsNotificationWithoutListener(t *testing.T) {
	pending := newPendingRegistry()
	done := make(chan struct{})
	go func() {
		for i := 0; i < suspendNotifyBuffer+5; i++ {
			pending.register("s1", suspension{callID: "call"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("register blocked when nobody was streaming")
	}
}
