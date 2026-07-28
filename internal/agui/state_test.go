package agui

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

func newTestTracker() *stateTracker {
	return newStateTracker("t1", "sess1", config.AgentCoder,
		models.Model{ID: "m1", Name: "Model One", Provider: "test", ContextWindow: 1000}, nil)
}

// TestStateSnapshotShape pins the JSON pointer paths the deltas patch. A field
// renamed here silently breaks every client's useCoAgent selector.
func TestStateSnapshotShape(t *testing.T) {
	snap, ok := newTestTracker().Snapshot().(StateSnapshotEvent)
	if !ok {
		t.Fatal("Snapshot must produce a STATE_SNAPSHOT event")
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Type     EventType `json:"type"`
		Snapshot struct {
			Thread     string           `json:"thread"`
			Session    string           `json:"session"`
			Agent      string           `json:"agent"`
			Model      ModelState       `json:"model"`
			Todos      []tools.TodoItem `json:"todos"`
			TokenUsage *TokenUsageState `json:"tokenUsage"`
			Files      []FileState      `json:"files"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != EventStateSnapshot {
		t.Fatalf("type = %q", decoded.Type)
	}
	if decoded.Snapshot.Session != "sess1" || decoded.Snapshot.Agent != "coder" {
		t.Fatalf("unexpected snapshot: %+v", decoded.Snapshot)
	}
	if decoded.Snapshot.Model.ID != "m1" || decoded.Snapshot.Model.ContextWindow != 1000 {
		t.Fatalf("model not projected: %+v", decoded.Snapshot.Model)
	}
	// The arrays must exist so a `replace`/`add` patch has a target.
	if decoded.Snapshot.Todos == nil || decoded.Snapshot.Files == nil {
		t.Fatal("todos and files must be present as empty arrays, not null")
	}
}

func TestStateTodosDelta(t *testing.T) {
	tracker := newTestTracker()
	todos := []tools.TodoItem{{Content: "build", Status: "pending", Priority: "high"}}

	got := tracker.Observe(agent.AgentEvent{Type: agent.AgentEventTypeTodosUpdated, Todos: todos})
	if len(got) != 1 {
		t.Fatalf("expected one delta, got %d", len(got))
	}
	delta, ok := got[0].(StateDeltaEvent)
	if !ok || len(delta.Delta) != 1 {
		t.Fatalf("unexpected event: %#v", got[0])
	}
	if delta.Delta[0].Op != "replace" || delta.Delta[0].Path != "/todos" {
		t.Fatalf("unexpected patch: %+v", delta.Delta[0])
	}

	// Re-emitting the same list must not produce a patch.
	if again := tracker.Observe(agent.AgentEvent{Type: agent.AgentEventTypeTodosUpdated, Todos: todos}); len(again) != 0 {
		t.Fatalf("identical todos produced %d events", len(again))
	}
}

// TestStateTokenUsageSuppressesRepeats matters because token usage is re-emitted
// as an estimate on every tool call: without deduplication the state channel
// would carry more traffic than the message channel.
func TestStateTokenUsageSuppressesRepeats(t *testing.T) {
	tracker := newTestTracker()
	usage := &agent.TokenUsageInfo{PromptTokens: 10, CompletionTokens: 2, ContextWindow: 1000, Estimated: true}

	if got := tracker.Observe(agent.AgentEvent{Type: agent.AgentEventTypeTokenUsage, TokenUsage: usage}); len(got) != 1 {
		t.Fatalf("expected one delta, got %d", len(got))
	}
	if got := tracker.Observe(agent.AgentEvent{Type: agent.AgentEventTypeTokenUsage, TokenUsage: usage}); len(got) != 0 {
		t.Fatalf("identical usage produced %d events", len(got))
	}

	usage2 := *usage
	usage2.CompletionTokens = 5
	got := tracker.Observe(agent.AgentEvent{Type: agent.AgentEventTypeTokenUsage, TokenUsage: &usage2})
	if len(got) != 1 {
		t.Fatalf("changed usage produced %d events", len(got))
	}
	if delta := got[0].(StateDeltaEvent); delta.Delta[0].Path != "/tokenUsage" {
		t.Fatalf("unexpected path %q", delta.Delta[0].Path)
	}
}

func TestStateFilesFromToolResults(t *testing.T) {
	tracker := newTestTracker()
	read := agent.AgentEvent{
		Type: agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{
			ToolCallID: "c1", Name: "view", Input: `{"file_path":"/repo/main.go"}`,
		},
	}
	got := tracker.Observe(read)
	if len(got) != 1 {
		t.Fatalf("expected a file delta, got %d events", len(got))
	}
	delta := got[0].(StateDeltaEvent)
	if delta.Delta[0].Op != "add" || delta.Delta[0].Path != "/files/-" {
		t.Fatalf("unexpected patch: %+v", delta.Delta[0])
	}
	if entry := delta.Delta[0].Value.(FileState); entry.Name != "main.go" || entry.Action != FileActionRead {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	// The same file read again changes nothing.
	if again := tracker.Observe(read); len(again) != 0 {
		t.Fatalf("repeated read produced %d events", len(again))
	}

	// Editing it upgrades the entry in place rather than appending a duplicate.
	edit := agent.AgentEvent{
		Type: agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{
			ToolCallID: "c2", Name: "edit", Input: `{"file_path":"/repo/main.go"}`,
		},
	}
	got = tracker.Observe(edit)
	if len(got) != 1 {
		t.Fatalf("expected an update, got %d events", len(got))
	}
	if op := got[0].(StateDeltaEvent).Delta[0]; op.Op != "replace" || op.Path != "/files/0" {
		t.Fatalf("unexpected patch: %+v", op)
	}
}

func TestStateIgnoresFailedAndNonFileTools(t *testing.T) {
	tracker := newTestTracker()

	failed := agent.AgentEvent{
		Type: agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{
			ToolCallID: "c1", Name: "edit", Input: `{"file_path":"/repo/a.go"}`, IsError: true,
		},
	}
	if got := tracker.Observe(failed); len(got) != 0 {
		t.Fatalf("a failed edit must not enter the file list, got %d events", len(got))
	}

	other := agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "c2", Name: "bash", Input: `{"command":"ls"}`},
	}
	if got := tracker.Observe(other); len(got) != 0 {
		t.Fatalf("a non-file tool must not enter the file list, got %d events", len(got))
	}
}

// TestStateFallsBackToToolCallInput covers the path where the result carries no
// input snapshot: the arguments are recovered from the call event instead.
func TestStateFallsBackToToolCallInput(t *testing.T) {
	tracker := newTestTracker()
	tracker.Observe(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: "c1", Name: "write", Input: `{"file_path":"/repo/new.go"}`, Finished: true},
	})
	got := tracker.Observe(agent.AgentEvent{
		Type:       agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{ToolCallID: "c1"},
	})
	if len(got) != 1 {
		t.Fatalf("expected a file delta, got %d events", len(got))
	}
	if entry := got[0].(StateDeltaEvent).Delta[0].Value.(FileState); entry.Action != FileActionWrite {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

// TestTranslatorRoutesStateEvents pins the division of labour: with a tracker
// attached, todos and token usage travel as state, not as custom events.
func TestTranslatorRoutesStateEvents(t *testing.T) {
	withState := newTranslator("t1", "r1").withState(newTestTracker())
	got := withState.Translate(agent.AgentEvent{
		Type:  agent.AgentEventTypeTodosUpdated,
		Todos: []tools.TodoItem{{Content: "x", Status: "pending"}},
	})
	if len(got) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(got))
	}
	if _, ok := got[0].(StateDeltaEvent); !ok {
		t.Fatalf("expected a STATE_DELTA, got %#v", got[0])
	}

	// Without a tracker the translator stays standalone and namespaces the event.
	bare := newTranslator("t1", "r1")
	got = bare.Translate(agent.AgentEvent{Type: agent.AgentEventTypeTodosUpdated})
	if custom, ok := got[0].(CustomEvent); !ok || custom.Name != "pando.todos" {
		t.Fatalf("expected the custom fallback, got %#v", got[0])
	}
}

func TestFilePathFromToolInput(t *testing.T) {
	cases := map[string]string{
		`{"file_path":"/a/b.go"}`: "/a/b.go",
		`{"path":"/a/c.go"}`:      "/a/c.go",
		`{"command":"ls"}`:        "",
		`not json`:                "",
		``:                        "",
	}
	for input, want := range cases {
		if got := filePathFromToolInput(input); got != want {
			t.Fatalf("filePathFromToolInput(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestStateStoreKeepsDocumentAcrossRuns pins that a thread's board survives its
// turns: files, todos and sub-agents are properties of the conversation, and a
// per-run document made them blink out on every new message.
func TestStateStoreKeepsDocumentAcrossRuns(t *testing.T) {
	store := newStateStore()
	model := models.Model{ID: "m1", Name: "Model One", ContextWindow: 1000}

	first := store.get("t1", "sess1", config.AgentCoder, model, nil)
	toolResult(first, "c1", "write", `{"file_path":"/repo/notes.txt"}`, "ok")

	newer := models.Model{ID: "m2", Name: "Model Two", ContextWindow: 2000}
	second := store.get("t1", "sess1", config.AgentCoder, newer, nil)
	if second != first {
		t.Fatal("the same thread must keep its document")
	}

	snap := second.Snapshot().(StateSnapshotEvent).Snapshot.(StateDoc)
	if len(snap.Files) != 1 || snap.Files[0].Name != "notes.txt" {
		t.Fatalf("the previous turn's files must still be there: %+v", snap.Files)
	}
	if snap.Model.ID != "m2" {
		t.Fatalf("a run with another model must rebind it: %+v", snap.Model)
	}

	// A rebound thread starts over: its files belong to a session that is gone.
	rebound := store.get("t1", "sess2", config.AgentCoder, model, nil)
	if rebound == first {
		t.Fatal("a new session must get a new document")
	}
	if files := rebound.Snapshot().(StateSnapshotEvent).Snapshot.(StateDoc).Files; len(files) != 0 {
		t.Fatalf("the new document must be empty: %+v", files)
	}
}

func TestStateStoreEvictsLeastRecentlyUsed(t *testing.T) {
	store := newStateStore()
	model := models.Model{ID: "m1"}
	for i := range maxStateThreads + 10 {
		store.get(threadName(i), "sess", config.AgentCoder, model, nil)
	}
	if len(store.byThread) != maxStateThreads {
		t.Fatalf("the store must stay bounded, holds %d", len(store.byThread))
	}
	if _, ok := store.byThread[threadName(0)]; ok {
		t.Fatal("the oldest thread should have been evicted")
	}
	if _, ok := store.byThread[threadName(maxStateThreads+9)]; !ok {
		t.Fatal("the newest thread must be kept")
	}
}

func threadName(i int) string { return "t" + strconv.Itoa(i) }
