package models

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// TestTaskDelegationJSONRoundTrip verifies that the delegation correlation
// fields and the embedded Conclusion survive a marshal -> unmarshal cycle.
func TestTaskDelegationJSONRoundTrip(t *testing.T) {
	started := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	completed := time.Date(2026, 6, 21, 10, 5, 0, 0, time.UTC)
	exit := 0

	orig := Task{
		ID:              "task-123",
		Prompt:          "do the thing",
		WorkDir:         "/tmp/proj",
		Status:          TaskStatusCompleted,
		Engine:          EnginePando,
		Model:           "copilot.gpt-5.4",
		ExitCode:        &exit,
		CreatedAt:       started,
		StartedAt:       &started,
		CompletedAt:     &completed,
		ParentSessionID: "sess-abc",
		ParentTaskID:    "task-parent",
		CorrelationID:   "corr-xyz",
		ProjectID:       "proj-1",
		ProjectPath:     "/tmp/proj",
		Depth:           2,
		Conclusion: &Conclusion{
			Status:      "success",
			Summary:     "completed successfully",
			Artifacts:   []string{"out.txt", "kb:doc-1"},
			MemoryRefs:  []string{"mem-key-1"},
			FollowUp:    "review later",
			Confidence:  0.92,
			Synthesized: true,
			CapturedAt:  completed,
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round-trip mismatch:\n orig=%+v\n  got=%+v", orig, got)
	}
}

// TestConclusionJSONTags asserts the on-disk JSON keys for the conclusion match
// the contract in the plan (snake_case for captured_at/memory_refs/follow_up).
func TestConclusionJSONTags(t *testing.T) {
	c := Conclusion{
		Status:     "partial",
		Summary:    "halfway",
		MemoryRefs: []string{"k1"},
		FollowUp:   "finish",
		CapturedAt: time.Unix(0, 0).UTC(),
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	for _, key := range []string{"status", "summary", "memory_refs", "follow_up", "captured_at"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q in %v", key, m)
		}
	}
}

// TestSpawnRequestDelegationFields ensures SpawnRequest carries the correlation
// fields that flow into a Task.
func TestSpawnRequestDelegationFields(t *testing.T) {
	req := SpawnRequest{
		Prompt:          "p",
		ParentSessionID: "sess",
		ParentTaskID:    "ptask",
		CorrelationID:   "corr",
		ProjectID:       "pid",
		ProjectPath:     "/p",
		Depth:           1,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SpawnRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ParentSessionID != "sess" || got.CorrelationID != "corr" || got.ProjectPath != "/p" || got.Depth != 1 {
		t.Fatalf("spawn request delegation fields not preserved: %+v", got)
	}
}
