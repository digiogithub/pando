package agui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

// toolResult feeds one successful tool result through the tracker, mimicking
// what the runtime observes for a real call.
func toolResult(tr *stateTracker, callID, name, input, content string) []Event {
	tr.Observe(agent.AgentEvent{
		Type:     agent.AgentEventTypeToolCall,
		ToolCall: &message.ToolCall{ID: callID, Name: name, Input: input, Finished: true},
	})
	return tr.Observe(agent.AgentEvent{
		Type: agent.AgentEventTypeToolResult,
		ToolResult: &message.ToolResult{
			ToolCallID: callID,
			Name:       name,
			Content:    content,
		},
	})
}

func subAgents(t *testing.T, tr *stateTracker) []SubAgentState {
	t.Helper()
	snap, ok := tr.Snapshot().(StateSnapshotEvent)
	if !ok {
		t.Fatal("Snapshot must produce a STATE_SNAPSHOT event")
	}
	doc, ok := snap.Snapshot.(StateDoc)
	if !ok {
		t.Fatalf("unexpected snapshot payload %T", snap.Snapshot)
	}
	return doc.SubAgents
}

// TestSubAgentSpawnAndProgress is the P7 contract: a delegated task appears on
// the board when it is spawned and advances in place when the agent looks it up,
// without the adapter ever talking to the orchestrator.
func TestSubAgentSpawnAndProgress(t *testing.T) {
	tr := newTestTracker()

	events := toolResult(tr, "c1", toolMesnadaSpawn,
		`{"prompt":"audit the auth middleware","engine":"claude"}`,
		`{"task_id":"task-1","status":"running","work_dir":"/repo"}`)
	if len(events) != 1 {
		t.Fatalf("spawn must produce exactly one delta, got %d", len(events))
	}
	delta, ok := events[0].(StateDeltaEvent)
	if !ok || delta.Delta[0].Op != "add" || delta.Delta[0].Path != "/subAgents/-" {
		t.Fatalf("unexpected delta: %+v", events[0])
	}

	got := subAgents(t, tr)
	if len(got) != 1 || got[0].ID != "task-1" || got[0].Status != "running" {
		t.Fatalf("board = %+v", got)
	}
	if got[0].Prompt != "audit the auth middleware" {
		t.Fatalf("the prompt must be recovered from the call input: %q", got[0].Prompt)
	}

	// A later lookup replaces the entry in place instead of appending a second one.
	events = toolResult(tr, "c2", toolMesnadaGet, `{"task_id":"task-1"}`,
		`{"task":{"id":"task-1","status":"completed","engine":"claude","model":"sonnet",
		  "conclusion":{"status":"success","summary":"two findings"}}}`)
	if len(events) != 1 {
		t.Fatalf("lookup must produce one delta, got %d", len(events))
	}
	delta, _ = events[0].(StateDeltaEvent)
	if delta.Delta[0].Op != "replace" || delta.Delta[0].Path != "/subAgents/0" {
		t.Fatalf("a known task must be patched in place: %+v", delta.Delta[0])
	}

	got = subAgents(t, tr)
	if len(got) != 1 {
		t.Fatalf("a lookup must not duplicate the entry: %+v", got)
	}
	if got[0].Status != "completed" || got[0].Conclusion != "success" || got[0].Summary != "two findings" {
		t.Fatalf("conclusion not projected: %+v", got[0])
	}
	// The status update carried no prompt; the known one must survive.
	if got[0].Prompt != "audit the auth middleware" {
		t.Fatalf("an update erased a field it did not report: %+v", got[0])
	}
}

// TestSubAgentListOnlyRefreshesKnownTasks: mesnada_list_tasks queries the whole
// store. The board is what this conversation delegated, not a global task table.
func TestSubAgentListOnlyRefreshesKnownTasks(t *testing.T) {
	tr := newTestTracker()
	toolResult(tr, "c1", toolMesnadaSpawn, `{"prompt":"mine"}`,
		`{"task_id":"task-1","status":"pending"}`)

	toolResult(tr, "c2", toolMesnadaList, `{}`,
		`{"tasks":[{"id":"task-1","status":"running"},{"id":"other-9","status":"running"}],"total":2}`)

	got := subAgents(t, tr)
	if len(got) != 1 {
		t.Fatalf("a listing must not adopt foreign tasks: %+v", got)
	}
	if got[0].Status != "running" {
		t.Fatalf("a known task must still be refreshed: %+v", got[0])
	}
}

// TestSubAgentSwarmTopology: a swarm publishes its whole shape at once, so the
// page can render the fan-out before any worker has reported.
func TestSubAgentSwarmTopology(t *testing.T) {
	tr := newTestTracker()
	toolResult(tr, "c1", toolMesnadaSwarm, `{}`, `{
		"swarm_id":"sess1",
		"worker_ids":["w1","w2"],
		"verifier_id":"v1",
		"synthesizer_id":"s1"}`)

	got := subAgents(t, tr)
	if len(got) != 4 {
		t.Fatalf("want 4 swarm members, got %+v", got)
	}
	roles := map[string]string{}
	for _, entry := range got {
		roles[entry.ID] = entry.Role
		if entry.Status != "pending" {
			t.Fatalf("a fresh swarm member must be pending: %+v", entry)
		}
	}
	if roles["w1"] != subAgentRoleWorker || roles["v1"] != subAgentRoleVerifier ||
		roles["s1"] != subAgentRoleSynthesizer {
		t.Fatalf("roles not assigned: %+v", roles)
	}
}

func TestSubAgentAwaitMarksTasks(t *testing.T) {
	tr := newTestTracker()
	toolResult(tr, "c1", toolMesnadaSpawn, `{"prompt":"x"}`, `{"task_id":"task-1","status":"running"}`)
	toolResult(tr, "c2", toolMesnadaAwait, `{"task_ids":["task-1"],"policy":"all"}`,
		"Await registered for 1 task")

	got := subAgents(t, tr)
	if len(got) != 1 || got[0].Status != "awaited" {
		t.Fatalf("await must mark the task: %+v", got)
	}
}

// TestSubAgentNonDelegationToolUnaffected pins that the file rules still run for
// every other tool.
func TestSubAgentNonDelegationToolUnaffected(t *testing.T) {
	tr := newTestTracker()
	events := toolResult(tr, "c1", "view", `{"file_path":"/repo/main.go"}`, "contents")
	if len(events) != 1 {
		t.Fatalf("a file tool must still emit its delta, got %d", len(events))
	}
	if len(subAgents(t, tr)) != 0 {
		t.Fatal("a file tool must not land on the sub-agent board")
	}
}

// TestSubAgentBoardIsBounded: the whole document is re-sent on every run, so the
// list cannot grow without bound.
func TestSubAgentBoardIsBounded(t *testing.T) {
	tr := newTestTracker()
	for i := range maxSubAgents + 10 {
		id := "task-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		tr.mu.Lock()
		tr.upsertSubAgentLocked(SubAgentState{ID: id, Status: "pending"})
		tr.mu.Unlock()
	}
	if got := len(subAgents(t, tr)); got != maxSubAgents {
		t.Fatalf("board size = %d, want %d", got, maxSubAgents)
	}
}

// TestSubAgentProseResultIsIgnored: a mesnada tool that answered with text (not
// JSON) must not corrupt the board or crash the run.
func TestSubAgentProseResultIsIgnored(t *testing.T) {
	tr := newTestTracker()
	events := toolResult(tr, "c1", toolMesnadaGet, `{"task_id":"nope"}`, "task not found")
	if len(events) != 0 {
		t.Fatalf("prose must produce no delta, got %+v", events)
	}
	if len(subAgents(t, tr)) != 0 {
		t.Fatal("prose must not create an entry")
	}
}

// TestStateSnapshotIncludesSubAgentsArray: an absent array is an invalid target
// for the `add` patch the first spawn emits.
func TestStateSnapshotIncludesSubAgentsArray(t *testing.T) {
	raw, err := json.Marshal(newTestTracker().Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Snapshot struct {
			SubAgents []SubAgentState `json:"subAgents"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Snapshot.SubAgents == nil {
		t.Fatal("subAgents must serialize as [], not null")
	}
}

// TestSubAgentSpawnInToolOutputFormat is the regression the unit tests missed:
// the mesnada tools answer through tools.NewStructuredResponse, which renders
// TOON (or TOML), never the JSON these tests used to assume. Feeding the real
// formatter's output through the tracker keeps the two in step.
func TestSubAgentSpawnInToolOutputFormat(t *testing.T) {
	content := tools.FormatStructuredData(map[string]any{
		"task_id":  "task-7fae1118",
		"status":   "running",
		"work_dir": "/repo",
	})
	if strings.HasPrefix(strings.TrimSpace(content), "{") {
		t.Fatalf("the formatter no longer produces non-JSON output, so this test guards nothing: %q", content)
	}

	tr := newTestTracker()
	toolResult(tr, "c1", toolMesnadaSpawn, `{"prompt":"count the files"}`, content)

	got := subAgents(t, tr)
	if len(got) != 1 || got[0].ID != "task-7fae1118" || got[0].Status != "running" {
		t.Fatalf("a structured spawn result must reach the board: %+v", got)
	}
	if got[0].Prompt != "count the files" {
		t.Fatalf("the prompt comes from the call arguments: %+v", got[0])
	}
}

// TestSubAgentTaskLookupInToolOutputFormat covers the nested shape: mesnada_get_task
// wraps a whole task, including its conclusion.
func TestSubAgentTaskLookupInToolOutputFormat(t *testing.T) {
	tr := newTestTracker()
	toolResult(tr, "c1", toolMesnadaSpawn, `{"prompt":"p"}`,
		tools.FormatStructuredData(map[string]any{"task_id": "task-1", "status": "running"}))

	toolResult(tr, "c2", toolMesnadaGet, `{"task_id":"task-1"}`,
		tools.FormatStructuredData(map[string]any{
			"task": map[string]any{
				"id":     "task-1",
				"status": "completed",
				"conclusion": map[string]any{
					"status":  "success",
					"summary": "12 files",
				},
			},
		}))

	got := subAgents(t, tr)
	if len(got) != 1 {
		t.Fatalf("the lookup must update in place: %+v", got)
	}
	if got[0].Status != "completed" || got[0].Conclusion != "success" || got[0].Summary != "12 files" {
		t.Fatalf("the task's own fields must reach the board: %+v", got[0])
	}
}
