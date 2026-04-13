package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	mesnadaModels "github.com/digiogithub/pando/pkg/mesnada/models"
)

// TaskResponse is the JSON representation of an orchestrator task returned by the API.
type TaskResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Agent     string    `json:"agent"`
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	Progress  int       `json:"progress"`
	Tokens    int       `json:"tokens"`
	Output    string    `json:"output,omitempty"`
	CurrentTool string   `json:"current_tool,omitempty"`
	ToolCalls []*mesnadaModels.ToolCall `json:"tool_calls,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// taskToResponse converts a models.Task into a TaskResponse.
func taskToResponse(t *mesnadaModels.Task) TaskResponse {
	progress := 0
	if t.Progress != nil {
		progress = t.Progress.Percentage
	}

	updatedAt := t.CreatedAt
	if t.CompletedAt != nil {
		updatedAt = *t.CompletedAt
	} else if t.StartedAt != nil {
		updatedAt = *t.StartedAt
	}

	return TaskResponse{
		ID:        t.ID,
		Name:      truncateTaskPrompt(t.Prompt, 80),
		Agent:     string(t.Engine),
		Model:     t.Model,
		Status:    string(t.Status),
		Progress:  progress,
		Output:    t.Output,
		CurrentTool: t.CurrentTool,
		ToolCalls: t.ToolCalls,
		CreatedAt: t.CreatedAt,
		UpdatedAt: updatedAt,
	}
}

func truncateTaskPrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// handleGetTasks returns the list of orchestrator tasks.
// GET /api/v1/orchestrator/tasks
func (s *Server) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.MesnadaOrchestrator == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tasks": []TaskResponse{},
			"total": 0,
		})
		return
	}

	tasks, err := s.app.MesnadaOrchestrator.ListTasks(mesnadaModels.ListRequest{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks: "+err.Error())
		return
	}

	responses := make([]TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		responses = append(responses, taskToResponse(t))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": responses,
		"total": len(responses),
	})
}

// CreateTaskRequest is the body for POST /api/v1/orchestrator/tasks.
type CreateTaskRequest struct {
	Prompt       string   `json:"prompt"`
	WorkDir      string   `json:"work_dir,omitempty"`
	Model        string   `json:"model,omitempty"`
	Engine       string   `json:"engine,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Background   bool     `json:"background"`
}

// handleCreateTask creates a new orchestrator task.
// POST /api/v1/orchestrator/tasks
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.MesnadaOrchestrator == nil {
		writeError(w, http.StatusServiceUnavailable, "orchestrator is not enabled")
		return
	}

	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	spawnReq := mesnadaModels.SpawnRequest{
		Prompt:       req.Prompt,
		WorkDir:      req.WorkDir,
		Model:        req.Model,
		Engine:       mesnadaModels.Engine(req.Engine),
		Tags:         req.Tags,
		Dependencies: req.Dependencies,
		Background:   true, // always run in background from the API
	}

	task, err := s.app.MesnadaOrchestrator.Spawn(r.Context(), spawnReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create task: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, taskToResponse(task))
}

// handleGetTaskByID returns the detail of a single orchestrator task.
// GET /api/v1/orchestrator/tasks/{id}
func (s *Server) handleGetTaskByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.MesnadaOrchestrator == nil {
		writeError(w, http.StatusServiceUnavailable, "orchestrator is not enabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	task, err := s.app.MesnadaOrchestrator.GetTask(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(task))
}

// handleDeleteTask cancels (if running) and removes a task from the store.
// DELETE /api/v1/orchestrator/tasks/{id}
func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.MesnadaOrchestrator == nil {
		writeError(w, http.StatusServiceUnavailable, "orchestrator is not enabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	if err := s.app.MesnadaOrchestrator.Delete(id); err != nil {
		if strings.Contains(err.Error(), "task not found") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete task: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleCancelTask cancels an in-progress task without removing it.
// POST /api/v1/orchestrator/tasks/{id}/cancel
func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.MesnadaOrchestrator == nil {
		writeError(w, http.StatusServiceUnavailable, "orchestrator is not enabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	if err := s.app.MesnadaOrchestrator.Cancel(id); err != nil {
		if strings.Contains(err.Error(), "task not found") {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if strings.Contains(err.Error(), "already in terminal state") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to cancel task: "+err.Error())
		return
	}

	task, err := s.app.MesnadaOrchestrator.GetTask(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated task")
		return
	}

	writeJSON(w, http.StatusOK, taskToResponse(task))
}
