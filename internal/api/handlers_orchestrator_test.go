package api

import (
	"encoding/json"
	"testing"
	"time"

	mesnadaModels "github.com/digiogithub/pando/pkg/mesnada/models"
)

// TestTaskToResponseSurfacesModelAndPersona verifies the orchestrator task DTO
// carries the per-task model and persona so the panel (WebUI/TUI) can surface
// them (item E2).
func TestTaskToResponseSurfacesModelAndPersona(t *testing.T) {
	task := &mesnadaModels.Task{
		ID:      "task-1",
		Prompt:  "do the thing",
		Engine:  mesnadaModels.Engine("claude"),
		Model:   "anthropic.claude-opus-4-8",
		Persona: "software-engineer",
		Status:  mesnadaModels.TaskStatusRunning,
	}

	resp := taskToResponse(task)

	if resp.Model != "anthropic.claude-opus-4-8" {
		t.Errorf("Model = %q, want %q", resp.Model, "anthropic.claude-opus-4-8")
	}
	if resp.Persona != "software-engineer" {
		t.Errorf("Persona = %q, want %q", resp.Persona, "software-engineer")
	}
}

// TestTaskToResponseOmitsEmptyPersona verifies an unset persona stays empty so
// the json `omitempty` tag drops it from the payload.
func TestTaskToResponseOmitsEmptyPersona(t *testing.T) {
	task := &mesnadaModels.Task{
		ID:     "task-2",
		Prompt: "no persona",
		Model:  "gpt-5.4",
		Status: mesnadaModels.TaskStatusPending,
	}

	if resp := taskToResponse(task); resp.Persona != "" {
		t.Errorf("Persona = %q, want empty", resp.Persona)
	}
}

// TestTaskToResponseFullFields verifies the additive fields are projected from
// models.Task and serialised under their snake_case keys.
func TestTaskToResponseFullFields(t *testing.T) {
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	done := started.Add(time.Minute)
	code := 1
	task := &mesnadaModels.Task{
		ID:              "task-3",
		Prompt:          "p",
		WorkDir:         "/w",
		Status:          mesnadaModels.TaskStatusFailed,
		Error:           "boom",
		ExitCode:        &code,
		OutputTail:      "tail",
		StartedAt:       &started,
		CompletedAt:     &done,
		Tags:            []string{"a"},
		Dependencies:    []string{"d"},
		ACPSessionID:    "acp-1",
		ACPMode:         "code",
		ParentSessionID: "sess-1",
		ProjectID:       "proj-1",
		ProjectPath:     "/w",
		ProjectName:     "w",
		Conclusion:      &mesnadaModels.Conclusion{},
	}
	b, err := json.Marshal(taskToResponse(task))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"work_dir", "tags", "dependencies", "error", "exit_code", "output_tail",
		"started_at", "completed_at", "acp_session_id", "acp_mode", "parent_session_id",
		"project_id", "project_path", "project_name", "conclusion"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
}

// TestTaskToResponseMinimalPayloadUnchanged verifies a bare task emits none of
// the new optional keys (backward compatibility).
func TestTaskToResponseMinimalPayloadUnchanged(t *testing.T) {
	b, _ := json.Marshal(taskToResponse(&mesnadaModels.Task{ID: "x", Status: mesnadaModels.TaskStatusPending}))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"work_dir", "tags", "error", "exit_code", "started_at", "completed_at",
		"acp_status", "project_id", "conclusion", "parent_session_id"} {
		if _, ok := m[k]; ok {
			t.Errorf("unexpected key %q", k)
		}
	}
}
