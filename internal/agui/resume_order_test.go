package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/message"
)

// sseFrames decodes every `data:` frame of an SSE body as a generic object.
func sseFrames(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatalf("undecodable frame %q: %v", line, err)
		}
		out = append(out, frame)
	}
	return out
}

// assertOpensLikeANewRun checks what @ag-ui/client's verifyEvents demands of
// every response (the first event is RUN_STARTED), plus the order runPrelude
// uses for a new run: RUN_STARTED for this segment's run id, then
// STATE_SNAPSHOT, and exactly one RUN_STARTED before the segment ends.
func assertOpensLikeANewRun(t *testing.T, body, runID string) {
	t.Helper()
	frames := sseFrames(t, body)
	if len(frames) < 2 {
		t.Fatalf("expected at least RUN_STARTED and STATE_SNAPSHOT, got %d frames:\n%s", len(frames), body)
	}
	if frames[0]["type"] != string(EventRunStarted) {
		t.Fatalf("first event = %v, want RUN_STARTED; types = %v", frames[0]["type"], frameTypes(body))
	}
	if frames[0]["runId"] != runID {
		t.Fatalf("RUN_STARTED runId = %v, want %q", frames[0]["runId"], runID)
	}
	if frames[1]["type"] != string(EventStateSnapshot) {
		t.Fatalf("second event = %v, want STATE_SNAPSHOT; types = %v", frames[1]["type"], frameTypes(body))
	}
	started := 0
	for _, f := range frames {
		if f["type"] == string(EventRunStarted) {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("RUN_STARTED appears %d times, want 1; types = %v", started, frameTypes(body))
	}
}

// finishOnceAttached ends run's segment once a request has subscribed to it.
// It polls instead of calling waitForSubscriberCount because t.Fatal must not
// run outside the test goroutine; a missing subscriber still fails the test,
// through the attach that never sees the run finish.
func finishOnceAttached(run *activeRun, events chan agent.AgentEvent) {
	for i := 0; i < 2000 && run.subscriberCount() < 1; i++ {
		time.Sleep(time.Millisecond)
	}
	events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
}

// TestResumedFrontendToolRunOpensWithRunStarted: the response to the POST that
// returns a frontend tool's result is a new AG-UI run, so it must open with
// that run's RUN_STARTED, not with the courtesy STATE_SNAPSHOT a reattach
// gets. CopilotKit rejected the old order with "First event must be
// 'RUN_STARTED'" and the agent's final answer never reached the page.
func TestResumedFrontendToolRunOpensWithRunStarted(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent, 4)
	suspend := make(chan suspension, 1)
	run := newSuspendableRun(events, suspend)
	r.runs.put(run)

	first := attachAndRecord(t, r, run, context.Background(), true, func() {
		events <- agent.AgentEvent{
			Type:     agent.AgentEventTypeToolCall,
			ToolCall: &message.ToolCall{ID: "call-1", Name: "showChart", Input: `{}`, Finished: true},
		}
		suspend <- suspension{callID: "call-1"}
	})
	if !strings.Contains(first.Body.String(), `"outcome":"interrupt"`) {
		t.Fatalf("setup: the first segment did not end in an interrupt: %s", first.Body.String())
	}

	// What the frontend tool does in production before the run suspends:
	// the call is pending, so the next POST carrying its result resumes.
	r.pending.register(run.sessionID, suspension{callID: "call-1"})

	in := &RunAgentInput{
		ThreadID: "t1", RunID: "r2",
		Messages: []Message{
			{ID: "m2", Role: RoleTool, ToolCallID: "call-1", Content: MessageContent{Text: "rendered"}},
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", nil)
	req.Header.Set("Authorization", "Bearer secret")
	done := make(chan struct{})
	go func() {
		r.handleExistingThreadRun(rec, req, run, in)
		close(done)
	}()
	waitForSubscriberCount(t, run, 1)
	events <- agent.AgentEvent{Type: agent.AgentEventTypeResponse}
	close(events)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the resumed segment never returned")
	}
	assertOpensLikeANewRun(t, rec.Body.String(), "r2")
	if !strings.Contains(rec.Body.String(), `"outcome":"success"`) {
		t.Fatalf("the resumed segment did not finish: %s", rec.Body.String())
	}
}

// TestReattachDuringFirstSegmentOpensWithRunStarted: a reattach to a run
// still in its first segment replays a buffer that holds no RUN_STARTED
// (runPrelude wrote it straight to the creating request), so the attach must
// rebuild it rather than open with STATE_SNAPSHOT or a bare replay.
func TestReattachDuringFirstSegmentOpensWithRunStarted(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	go r.pump(run)

	go finishOnceAttached(run, events)

	body := `{"threadId":"t1","runId":"r-reattach","messages":[{"id":"m1","role":"assistant","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, defaultPath+"/coder", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("agent", "coder")
	rec := httptest.NewRecorder()
	r.handleRun(rec, req)

	// The reattach observes the run it joined, so the run id is the live
	// segment's ("r1", see newSuspendableRun), not the request's.
	assertOpensLikeANewRun(t, rec.Body.String(), "r1")
	if !strings.Contains(rec.Body.String(), `"outcome":"success"`) {
		t.Fatalf("the reattach did not observe the run finishing: %s", rec.Body.String())
	}
}

// TestReattachToLossyBufferStillOpensWithRunStarted: the replayLossy marker is
// a CUSTOM event, so it too must come after RUN_STARTED.
func TestReattachToLossyBufferStillOpensWithRunStarted(t *testing.T) {
	r := newTestRuntime(testConfig(), "secret")
	events := make(chan agent.AgentEvent)
	run := newSuspendableRun(events, make(chan suspension, 1))
	r.runs.put(run)
	run.buffer.mu.Lock()
	run.buffer.lossy = true
	run.buffer.mu.Unlock()
	go r.pump(run)

	go finishOnceAttached(run, events)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, defaultPath+"/threads/t1/stream", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.SetPathValue("id", "t1")
	r.handleStream(rec, req)

	assertOpensLikeANewRun(t, rec.Body.String(), "r1")
	types := frameTypes(rec.Body.String())
	if len(types) < 3 || types[2] != string(EventCustom) {
		t.Fatalf("expected the replayLossy marker right after the prelude, got %v", types)
	}
}
