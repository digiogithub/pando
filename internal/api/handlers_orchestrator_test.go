package api

import (
	"testing"

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
