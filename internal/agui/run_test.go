package agui

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
)

// ---------------------------------------------------------------- eventBuffer

// TestEventBufferBoundedAndLossy is the PANDO-US-0017 acceptance criterion:
// the replay buffer is bounded, and once it has dropped an event it reports
// itself lossy rather than growing without limit.
func TestEventBufferBoundedAndLossy(t *testing.T) {
	b := newEventBuffer(3)

	got, lossy := b.snapshot()
	if len(got) != 0 || lossy {
		t.Fatalf("a fresh buffer must be empty and not lossy, got %d items lossy=%v", len(got), lossy)
	}

	b.append(NewRunStarted("t1", "r1"))
	got, lossy = b.snapshot()
	if len(got) != 1 || lossy {
		t.Fatalf("expected 1 item, not lossy; got %d lossy=%v", len(got), lossy)
	}

	for i := 0; i < 5; i++ {
		b.append(NewToolCallEnd(strconv.Itoa(i)))
	}
	got, lossy = b.snapshot()
	if len(got) != 3 {
		t.Fatalf("buffer must stay capped at its limit, got %d items", len(got))
	}
	if !lossy {
		t.Fatal("a buffer that dropped events must report itself lossy")
	}
	// The 3 most recent appends must be what survived (oldest dropped first).
	last, ok := got[2].(ToolCallEndEvent)
	if !ok || last.ToolCallID != "4" {
		t.Fatalf("expected the newest event to survive, got %#v", got[2])
	}
}

// -------------------------------------------------------- goroutine leak (US-0017)

// TestParkedRunExpiryLeavesNoGoroutine is the PANDO-US-0017 acceptance
// criterion "a goroutine-leak assertion passes after teardown": once a
// parked run's grace period expires with nobody reattached, its pump
// goroutine must exit — not sit blocked forever.
func TestParkedRunExpiryLeavesNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	r := newTestRuntime(testConfig(), "secret")
	r.cfg.DisconnectGrace = 5 * time.Millisecond
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
		t.Fatal("the parked run was never torn down")
	}

	// Give the now-exited pump goroutine a moment to actually unwind and be
	// reclaimed by the scheduler's bookkeeping before comparing counts.
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Fatalf("goroutine count grew from %d to %d after a parked run's teardown", before, got)
	}
}

// TestReattachClearsDisconnectGraceTimer is the PANDO-US-0018 acceptance
// criterion: a run parked by a disconnect is unparked by a reattach and its
// grace timer is cleared -- proven by outliving what would have been the
// original expiry with no teardown, because the reattach cancelled it.
func TestReattachClearsDisconnectGraceTimer(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	r.cfg.DisconnectGrace = 30 * time.Millisecond
	events := make(chan agent.AgentEvent, 1)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)
	t.Cleanup(func() { close(events) })

	// First attach disconnects immediately, arming the grace timer.
	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstCancel()
	rec1 := httptest.NewRecorder()
	sse1, _ := NewSSEWriter(rec1)
	r.attachRun(firstCtx, sse1, run, true)

	// Reattach well before the original timer would have fired.
	rec2 := httptest.NewRecorder()
	sse2, _ := NewSSEWriter(rec2)
	attach2Done := make(chan struct{})
	go func() { r.attachRun(context.Background(), sse2, run, false); close(attach2Done) }()
	waitForSubscriberCount(t, run, 1)

	// Outlive the ORIGINAL grace window; the run must still be alive because
	// the reattach cleared that timer.
	time.Sleep(100 * time.Millisecond)
	if _, ok := r.runs.get("t1"); !ok {
		t.Fatal("the reattach should have cleared the disconnect grace timer, but the run was torn down anyway")
	}

	events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	select {
	case <-attach2Done:
	case <-time.After(2 * time.Second):
		t.Fatal("the reattached stream never observed the run finishing")
	}
}

// TestSuspendedRunReaperUsesSuspendGraceNotDisconnectGrace is the PANDO-US-0017
// acceptance criterion "the suspended-run reaper's lifetime is unchanged": a
// run parked because it is suspended waiting on a frontend tool must still be
// governed by suspendGrace, even when Config.DisconnectGrace is much shorter
// -- the two lifetimes must not be conflated.
func TestSuspendedRunReaperUsesSuspendGraceNotDisconnectGrace(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	r.cfg.DisconnectGrace = time.Millisecond // deliberately far shorter than suspendGrace
	events := make(chan agent.AgentEvent, 4)
	suspend := make(chan suspension, 1)
	run := newSuspendableRun(events, suspend)
	r.runs.put(run)
	t.Cleanup(func() { close(events) })

	// attachAndRecord blocks until the interrupt passes through attachLoop's
	// segment-boundary detection and it detaches on its own -- exactly the
	// "last subscriber leaves a now-suspended run" case Runtime.attachLoop's
	// deferred park arms against, no explicit disconnect needed.
	rec := attachAndRecord(t, r, run, context.Background(), true, func() {
		suspend <- suspension{callID: "call-1", call: &toolCallInfo{name: "showChart"}}
	})
	if !strings.Contains(rec.Body.String(), `"outcome":"interrupt"`) {
		t.Fatal("the run never suspended")
	}

	// Config.DisconnectGrace (1ms) has long since elapsed; a suspended run
	// must NOT have been reaped by it.
	time.Sleep(50 * time.Millisecond)
	if _, ok := r.runs.get("t1"); !ok {
		t.Fatal("a suspended run must be governed by suspendGrace, not the much shorter Config.DisconnectGrace")
	}
}

// -------------------------------------------------------------- replay (US-0018)

// TestReattachReplaysBufferedEventsThenGoesLive is the PANDO-US-0018 core
// acceptance criterion: reconnecting mid-run replays what was missed (no
// duplicate TOOL_CALL_END, no orphaned TOOL_CALL_ARGS) and then continues
// live.
func TestReattachReplaysBufferedEventsThenGoesLive(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 8)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)

	events <- agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "hello "}
	events <- agent.AgentEvent{
		Type: agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{
			ID: "call-1", Name: "view", Input: `{"file_path":"a.go"}`, Finished: true,
		},
	}
	events <- agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "call-1", Name: "view", Content: "package main"},
	}

	// Give the pump a moment to translate and buffer these before attaching,
	// simulating a client that reattaches after missing the whole segment.
	waitForBuffered(t, run, 6) // START, CONTENT, TOOL_CALL_START, ARGS, END, RESULT

	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	attachDone := make(chan struct{})
	go func() {
		r.attachRun(context.Background(), sse, run, false)
		close(attachDone)
	}()

	// Now finish the run live, after the reattach has subscribed.
	waitForSubscriber(t, run)
	events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	close(events)

	select {
	case <-attachDone:
	case <-time.After(2 * time.Second):
		t.Fatal("attachRun never returned after the run finished")
	}

	body := rec.Body.String()
	types := frameTypes(body)

	var starts, ends, results int
	for _, ty := range types {
		switch ty {
		case string(EventToolCallStart):
			starts++
		case string(EventToolCallEnd):
			ends++
		case string(EventToolCallResult):
			results++
		}
	}
	if starts != 1 {
		t.Fatalf("expected exactly one TOOL_CALL_START on replay, got %d: %v", starts, types)
	}
	if ends != 1 {
		t.Fatalf("expected exactly one TOOL_CALL_END on replay (no duplicate), got %d: %v", ends, types)
	}
	if results != 1 {
		t.Fatalf("expected exactly one TOOL_CALL_RESULT, got %d: %v", results, types)
	}
	// ARGS must come after START and before END: never orphaned.
	startIdx := indexOf(types, string(EventToolCallStart))
	argsIdx := indexOf(types, string(EventToolCallArgs))
	endIdx := indexOf(types, string(EventToolCallEnd))
	if startIdx < 0 || argsIdx < 0 || endIdx < 0 || !(startIdx < argsIdx && argsIdx < endIdx) {
		t.Fatalf("TOOL_CALL_ARGS must fall between its START and END, got order %v", types)
	}
	if !strings.Contains(body, `"outcome":"success"`) {
		t.Fatalf("the run did not continue live to completion: %s", body)
	}
}

// TestSecondTabAttachesReadOnlyAndSeesLiveEvents is the PANDO-US-0018
// acceptance criterion: a second attach receives the same live events as the
// first and cannot submit tool results (it has no route to do so: only a
// POST carrying matching tool messages can, see resumeCandidates).
func TestSecondTabAttachesReadOnlyAndSeesLiveEvents(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)

	rec1 := httptest.NewRecorder()
	sse1, _ := NewSSEWriter(rec1)
	done1 := make(chan struct{})
	go func() { r.attachRun(context.Background(), sse1, run, true); close(done1) }()

	waitForSubscriberCount(t, run, 1)

	rec2 := httptest.NewRecorder()
	sse2, _ := NewSSEWriter(rec2)
	done2 := make(chan struct{})
	go func() { r.attachRun(context.Background(), sse2, run, false); close(done2) }()

	waitForSubscriberCount(t, run, 2)

	events <- agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "hi"}
	events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	close(events)

	for _, done := range []chan struct{}{done1, done2} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("an attach never detached after the run finished")
		}
	}

	for _, rec := range []*httptest.ResponseRecorder{rec1, rec2} {
		if !strings.Contains(rec.Body.String(), `"delta":"hi"`) {
			t.Fatalf("attach did not see the live content delta: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"outcome":"success"`) {
			t.Fatalf("attach did not see the run finish: %s", rec.Body.String())
		}
	}
}

// TestReattachToUnknownThreadIs404 pins the documented status for
// PANDO-US-0018's "attaching to a thread with no live run" case, at the
// handler level (GET {path}/threads/{id}/stream).
func TestReattachToUnknownThreadIs404(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", defaultPath+"/threads/ghost/stream", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "ghost")
	r.handleStream(rec, req)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// ----------------------------------------------------------------- cancel (US-0019)

// TestCancelReleasesPermissionWaitAndRemovesRun is the PANDO-US-0019
// acceptance criterion: cancelling a run parked on a permission prompt (which
// selects on the adapter's base context, not the run's -- see
// pendingRegistry.cancelAll's doc comment) returns promptly, the run is gone
// from runStore, and the blocked goroutine is released rather than left
// stuck.
func TestCancelReleasesPermissionWaitAndRemovesRun(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension))
	r.runs.put(run)
	go r.pump(run)
	run.setSuspended(true)

	// Simulate a permission prompt blocked exactly like awaitClient (hitl.go):
	// waiting on a pendingRegistry channel for run.sessionID.
	ch := r.pending.register(run.sessionID, suspension{callID: "perm-1"})
	unblocked := make(chan Message, 1)
	go func() {
		select {
		case msg := <-ch:
			unblocked <- msg
		case <-time.After(5 * time.Second):
		}
	}()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", defaultPath+"/runs/t1/cancel", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "t1")

	start := time.Now()
	r.handleCancelRun(rec, req)
	elapsed := time.Since(start)

	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if elapsed > cancelTeardownTimeout {
		t.Fatalf("cancel took %v, longer than its own timeout budget", elapsed)
	}
	if _, ok := r.runs.get("t1"); ok {
		t.Fatal("the run must be gone from runStore once cancel returns")
	}
	select {
	case msg := <-unblocked:
		if msg.Error == "" {
			t.Fatalf("expected the released wait to carry an error, got %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("the permission wait was never released: a goroutine is stuck")
	}
}

// TestCancelBroadcastsToEveryAttachedStreamAndCloses is the PANDO-US-0019
// acceptance criterion: every attached stream, live attach and read-only
// follower alike, receives RUN_ERROR{code:"cancelled"} and is closed.
func TestCancelBroadcastsToEveryAttachedStreamAndCloses(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)

	rec1 := httptest.NewRecorder()
	sse1, _ := NewSSEWriter(rec1)
	done1 := make(chan struct{})
	go func() { r.attachRun(context.Background(), sse1, run, true); close(done1) }()
	waitForSubscriberCount(t, run, 1)

	rec2 := httptest.NewRecorder()
	sse2, _ := NewSSEWriter(rec2)
	done2 := make(chan struct{})
	go func() { r.attachRun(context.Background(), sse2, run, false); close(done2) }()
	waitForSubscriberCount(t, run, 2)

	req := httptest.NewRequest("POST", defaultPath+"/runs/t1/cancel", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	r.handleCancelRun(rec, req)
	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	for _, done := range []chan struct{}{done1, done2} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("an attached stream was not closed by cancel")
		}
	}
	for _, rec := range []*httptest.ResponseRecorder{rec1, rec2} {
		body := rec.Body.String()
		if !strings.Contains(body, string(EventRunError)) || !strings.Contains(body, `"code":"cancelled"`) {
			t.Fatalf("expected RUN_ERROR{code:cancelled}, got: %s", body)
		}
	}
}

// TestCancelIsIdempotent is the PANDO-US-0019 acceptance criterion: a second
// cancel, and a cancel on an unknown or already-finished thread, answer
// success without error.
func TestCancelIsIdempotent(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")

	// Unknown thread.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", defaultPath+"/runs/ghost/cancel", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "ghost")
	r.handleCancelRun(rec, req)
	if rec.Code != 204 {
		t.Fatalf("unknown thread: status = %d, want 204", rec.Code)
	}

	// A live run, cancelled twice.
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension))
	r.runs.put(run)
	go r.pump(run)

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", defaultPath+"/runs/t1/cancel", nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.SetPathValue("id", "t1")
		r.handleCancelRun(rec, req)
		if rec.Code != 204 {
			t.Fatalf("cancel #%d: status = %d, want 204", i+1, rec.Code)
		}
	}
	if _, ok := r.runs.get("t1"); ok {
		t.Fatal("the run must be gone from runStore")
	}
}

// TestCancelDoesNotTouchThreadBindingOrMessages is the PANDO-US-0019
// acceptance criterion: cancel ends the run, not the thread — the
// agui_threads binding and the session's messages are untouched, so the
// thread stays listable and its messages readable afterward.
func TestCancelDoesNotTouchThreadBindingOrMessages(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	r.threads.put(context.Background(), "t1", "sess1", "coder")
	messages := newFakeMessageService()
	seedUserMessage(messages, "sess1", "m1", "hello")
	r.deps.Messages = messages

	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension))
	run.sessionID = "sess1"
	r.runs.put(run)
	go r.pump(run)

	req := httptest.NewRequest("POST", defaultPath+"/runs/t1/cancel", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	r.handleCancelRun(rec, req)
	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	sessionID, ok := r.threads.get(context.Background(), "t1")
	if !ok || sessionID != "sess1" {
		t.Fatalf("cancel must not remove the thread binding, got ok=%v session=%q", ok, sessionID)
	}
	msgs, err := r.deps.Messages.List(context.Background(), "sess1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("cancel must not delete the session's messages, got %d", len(msgs))
	}
}

// TestCancelFromDifferentRequestThanTheStream is the PANDO-US-0019
// acceptance criterion: cancelling from a request other than the one
// streaming the run works. The streaming attach here runs on its own
// goroutine with its own (never-cancelled) context, exactly like a second
// browser tab issuing the cancel.
func TestCancelFromDifferentRequestThanTheStream(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)

	rec := httptest.NewRecorder()
	sse, _ := NewSSEWriter(rec)
	streamDone := make(chan struct{})
	go func() {
		r.attachRun(context.Background(), sse, run, true) // never-cancelled context
		close(streamDone)
	}()
	waitForSubscriberCount(t, run, 1)

	cancelReq := httptest.NewRequest("POST", defaultPath+"/runs/t1/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer secret")
	cancelReq.SetPathValue("id", "t1")
	cancelRec := httptest.NewRecorder()
	r.handleCancelRun(cancelRec, cancelReq)
	if cancelRec.Code != 204 {
		t.Fatalf("cancel status = %d, want 204", cancelRec.Code)
	}

	select {
	case <-streamDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream (a different request) was never closed by the cancel")
	}
	if !strings.Contains(rec.Body.String(), `"code":"cancelled"`) {
		t.Fatalf("the streaming request never saw the cancellation: %s", rec.Body.String())
	}
}

// waitForBuffered polls until the run's replay buffer holds at least n
// events, for tests that need the pump to have processed queued input before
// a reattach.
func waitForBuffered(t *testing.T, run *activeRun, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := run.replaySnapshot(); len(got) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("buffer never reached %d events", n)
}

func waitForSubscriber(t *testing.T, run *activeRun) {
	t.Helper()
	waitForSubscriberCount(t, run, 1)
}

func waitForSubscriberCount(t *testing.T, run *activeRun, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if run.subscriberCount() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("never reached %d subscriber(s)", n)
}
