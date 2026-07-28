package agui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

func sessionContext(sessionID string) context.Context {
	return context.WithValue(context.Background(), tools.SessionIDContextKey, sessionID)
}

// clientResult builds the tool message an AG-UI client sends back.
func clientResult(content string) Message {
	return Message{Role: RoleTool, Content: MessageContent{Text: content}}
}

// TestFrontendToolResolvedByClient walks the whole handoff: the proxy blocks,
// the registry announces the suspension, the client's result unblocks it.
func TestFrontendToolResolvedByClient(t *testing.T) {
	pending := newPendingRegistry()
	notify := pending.watch("sess1")
	tool := &frontendTool{
		info:    tools.ToolInfo{Name: "showChart"},
		pending: pending,
		timeout: 2 * time.Second,
	}

	done := make(chan tools.ToolResponse, 1)
	go func() {
		resp, err := tool.Run(sessionContext("sess1"), tools.ToolCall{ID: "call-1", Name: "showChart"})
		if err != nil {
			t.Errorf("Run returned an error: %v", err)
		}
		done <- resp
	}()

	select {
	case s := <-notify:
		if s.callID != "call-1" {
			t.Fatalf("suspension announced for %q", s.callID)
		}
		if len(s.events) != 0 {
			t.Fatal("a frontend tool's events come from the agent stream, not from the handler")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the suspension was never announced")
	}

	if !pending.waiting("sess1") {
		t.Fatal("the call should be registered as waiting")
	}
	if !pending.resolve("sess1", "call-1", clientResult("rendered")) {
		t.Fatal("resolve should find the waiting call")
	}

	select {
	case resp := <-done:
		if resp.Content != "rendered" || resp.IsError {
			t.Fatalf("unexpected response: %+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the tool never unblocked")
	}
}

// TestFrontendToolTimesOutAsToolError pins that a browser that never answers
// degrades into a tool error the model can reason about, not a failed run.
func TestFrontendToolTimesOutAsToolError(t *testing.T) {
	tool := &frontendTool{
		info:    tools.ToolInfo{Name: "showChart"},
		pending: newPendingRegistry(),
		timeout: 20 * time.Millisecond,
	}
	resp, err := tool.Run(sessionContext("sess1"), tools.ToolCall{ID: "call-1"})
	if err != nil {
		t.Fatalf("a client timeout must not fail the run: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "never returned a result") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestFrontendToolCancelledWithRun(t *testing.T) {
	tool := &frontendTool{
		info:    tools.ToolInfo{Name: "showChart"},
		pending: newPendingRegistry(),
		timeout: time.Minute,
	}
	ctx, cancel := context.WithCancel(sessionContext("sess1"))
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	if _, err := tool.Run(ctx, tools.ToolCall{ID: "call-1"}); err == nil {
		t.Fatal("cancelling the run must abort the pending tool call")
	}
}

func TestFrontendToolRequiresSession(t *testing.T) {
	tool := &frontendTool{info: tools.ToolInfo{Name: "showChart"}, pending: newPendingRegistry()}
	resp, err := tool.Run(context.Background(), tools.ToolCall{ID: "call-1"})
	if err != nil || !resp.IsError {
		t.Fatalf("a session-less call must report a tool error, got %+v (%v)", resp, err)
	}
}

func TestPendingRegistryIgnoresUnknownAndDuplicateResults(t *testing.T) {
	pending := newPendingRegistry()
	pending.watch("sess1")

	if pending.resolve("sess1", "nope", clientResult("x")) {
		t.Fatal("an unknown call id must not be reported as resolved")
	}
	pending.register("sess1", suspension{callID: "call-1"})
	if !pending.resolve("sess1", "call-1", clientResult("first")) {
		t.Fatal("the first delivery should win")
	}
	if pending.resolve("sess1", "call-1", clientResult("second")) {
		t.Fatal("a duplicated delivery must be ignored")
	}

	pending.discard("sess1")
	if pending.waiting("sess1") {
		t.Fatal("discard must clear the session")
	}
}

// TestNewFrontendToolsRejectsShadowing guards the security property: a page must
// not be able to redefine a real Pando tool by declaring one with its name.
func TestNewFrontendToolsRejectsShadowing(t *testing.T) {
	reserved := map[string]bool{"bash": true}
	got := newFrontendTools([]Tool{
		{Name: "bash"},
		{Name: ""},
		{Name: "showChart", Description: "render a chart"},
	}, reserved, newPendingRegistry())

	if len(got) != 1 {
		t.Fatalf("expected only the safe tool, got %d", len(got))
	}
	info := got[0].Info()
	if info.Name != "showChart" {
		t.Fatalf("unexpected tool %q", info.Name)
	}
	if !strings.Contains(info.Description, "render a chart") ||
		!strings.Contains(info.Description, "browser") {
		t.Fatalf("the description must keep the client text and state where it runs: %q", info.Description)
	}
}

func TestToolSchemaShapes(t *testing.T) {
	var full any
	if err := json.Unmarshal([]byte(`{
		"type":"object",
		"properties":{"title":{"type":"string"}},
		"required":["title"]
	}`), &full); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	props, required := toolSchema(full)
	if _, ok := props["title"]; !ok {
		t.Fatalf("properties not unwrapped: %v", props)
	}
	if len(required) != 1 || required[0] != "title" {
		t.Fatalf("required = %v", required)
	}

	// Some clients send the properties map directly.
	var bare any
	if err := json.Unmarshal([]byte(`{"title":{"type":"string"}}`), &bare); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	props, required = toolSchema(bare)
	if _, ok := props["title"]; !ok || required != nil {
		t.Fatalf("bare properties map mishandled: %v %v", props, required)
	}

	// An argument-less tool must yield an empty (non-nil) property map.
	var empty any
	if err := json.Unmarshal([]byte(`{"type":"object"}`), &empty); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if props, _ = toolSchema(empty); props == nil || len(props) != 0 {
		t.Fatalf("expected an empty property map, got %v", props)
	}
	if props, _ = toolSchema(nil); props == nil {
		t.Fatal("a nil schema must still produce a usable map")
	}
}

func TestToolResponseFromMessage(t *testing.T) {
	ok := toolResponseFromMessage(Message{Role: RoleTool, ToolCallID: "c1", Content: MessageContent{Text: "done"}})
	if ok.IsError || ok.Content != "done" {
		t.Fatalf("unexpected response: %+v", ok)
	}

	failed := toolResponseFromMessage(Message{Role: RoleTool, ToolCallID: "c1", Error: "user cancelled"})
	if !failed.IsError || !strings.Contains(failed.Content, "user cancelled") {
		t.Fatalf("a client error must reach the model as a tool error: %+v", failed)
	}

	empty := toolResponseFromMessage(Message{Role: RoleTool, ToolCallID: "c1"})
	if empty.IsError || empty.Content == "" {
		t.Fatalf("an empty result must still carry text: %+v", empty)
	}
}

// TestSuppressedToolCallIsNotEchoed pins the resumption rule: the client already
// executed and rendered the call, so the resumed run must stay silent about it.
func TestSuppressedToolCallIsNotEchoed(t *testing.T) {
	tr := newTranslator("t1", "r2")
	tr.suppressToolCall("call-1")

	got := tr.Translate(agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "call-1", Content: "rendered"},
	})
	if len(got) != 0 {
		t.Fatalf("expected no events for a client-resolved call, got %#v", got)
	}

	// A different call is unaffected. Its result also opens it: the stream never
	// carried the call, and a client that receives an END for a call it never saw
	// start rejects the frame.
	got = tr.Translate(agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "call-2", Name: "ls", Content: "ls"},
	})
	want := []EventType{EventToolCallStart, EventToolCallEnd, EventToolCallResult}
	if len(got) != len(want) {
		t.Fatalf("expected START + END + RESULT for an unrelated call, got %#v", got)
	}
	for i, ev := range got {
		if ev.EventType() != want[i] {
			t.Fatalf("event %d: got %s, want %s", i, ev.EventType(), want[i])
		}
	}
}

// TestRunStoreReplacement covers the two-tab case: a newer run for the same
// thread must not be evicted when the older one finishes.
func TestRunStoreReplacement(t *testing.T) {
	store := newRunStore()
	older := &activeRun{threadID: "t1", cancel: func() {}}
	newer := &activeRun{threadID: "t1", cancel: func() {}}

	store.put(older)
	store.put(newer)
	store.remove(older)

	got, ok := store.get("t1")
	if !ok || got != newer {
		t.Fatal("removing a superseded run must not evict its replacement")
	}
	store.remove(newer)
	if _, ok := store.get("t1"); ok {
		t.Fatal("the current run should have been removed")
	}
}

func TestActiveRunStopsOnce(t *testing.T) {
	var cancels int
	run := &activeRun{threadID: "t1", cancel: func() { cancels++ }}

	if !run.stop() {
		t.Fatal("the first stop should report that it did the work")
	}
	if run.stop() {
		t.Fatal("a second stop must be a no-op")
	}
	if cancels != 1 {
		t.Fatalf("cancel called %d times", cancels)
	}

	// Parking a stopped run must not resurrect its teardown timer.
	run.park(func() { t.Error("a stopped run must not be parked") })
	time.Sleep(10 * time.Millisecond)
}
