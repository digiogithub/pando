package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/project"
)

// projectResponse is the JSON wire format for a Project.
type projectResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Status      string `json:"status"`
	Initialized bool   `json:"initialized"`
	ACPPID      int    `json:"acp_pid,omitempty"`
	LastOpened  *int64 `json:"last_opened,omitempty"` // Unix seconds
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// toProjectResponse converts a domain Project to its JSON wire representation.
func toProjectResponse(p project.Project) projectResponse {
	resp := projectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Path:        p.Path,
		Status:      p.Status,
		Initialized: p.Initialized,
		ACPPID:      p.ACPPID,
		CreatedAt:   p.CreatedAt.Unix(),
		UpdatedAt:   p.UpdatedAt.Unix(),
	}
	if p.LastOpened != nil {
		v := p.LastOpened.Unix()
		resp.LastOpened = &v
	}
	return resp
}

// handleListProjects handles GET /api/v1/projects.
// Returns all registered projects as {"projects": [...]}.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	projects, err := s.app.ProjectManager.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]projectResponse, len(projects))
	for i, p := range projects {
		resp[i] = toProjectResponse(p)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects": resp,
	})
}

// handleCreateProject handles POST /api/v1/projects.
// Body: {"path": string, "name": string (optional)}.
// Returns 201 + {"project": ...}.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	p, err := s.app.ProjectManager.Register(r.Context(), req.Name, req.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"project": toProjectResponse(*p),
	})
}

// handleGetProject handles GET /api/v1/projects/{id}.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	p, err := s.app.Projects.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": toProjectResponse(*p),
	})
}

// handleDeleteProject handles DELETE /api/v1/projects/{id}.
// Returns 204 on success.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	if err := s.app.ProjectManager.Unregister(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleActivateProject handles POST /api/v1/projects/{id}/activate.
// Returns 409 Conflict with {"error":"project_needs_init","project_id":"...","path":"..."}
// when the project directory has no Pando configuration file.
func (s *Server) handleActivateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	err := s.app.ProjectManager.Activate(r.Context(), id)
	if err != nil {
		if errors.Is(err, project.ErrProjectNeedsInit) {
			// Retrieve path for the response body.
			var projPath string
			if p, getErr := s.app.Projects.Get(r.Context(), id); getErr == nil {
				projPath = p.Path
			}
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":      "project_needs_init",
				"project_id": id,
				"path":       projPath,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "activated",
		"project_id": id,
	})
}

// handleDeactivateProject handles POST /api/v1/projects/{id}/deactivate.
func (s *Server) handleDeactivateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	if err := s.app.ProjectManager.Deactivate(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "deactivated",
	})
}

// handleInitProject handles POST /api/v1/projects/{id}/init.
// Runs CompleteInit which writes config files then activates the project.
func (s *Server) handleInitProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	if err := s.app.ProjectManager.CompleteInit(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "initialized",
	})
}

// handleGetActiveProject handles GET /api/v1/projects/active.
// Returns {"project": null} when no project is active.
func (s *Server) handleGetActiveProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	p, err := s.app.ProjectManager.ActiveProject(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if p == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"project": nil,
		})
		return
	}

	resp := toProjectResponse(*p)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": resp,
	})
}

// handleRenameProject handles PATCH /api/v1/projects/{id}.
// Body: {"name": string}.
// Returns 200 + {"project": ...} on success.
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if err := s.app.ProjectManager.Rename(r.Context(), id, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	p, err := s.app.Projects.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": toProjectResponse(*p),
	})
}

// handleProjectEvents handles GET /api/v1/projects/events.
// It streams Server-Sent Events for project lifecycle changes.
func (s *Server) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.ProjectManager == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Subscribe to project manager events.
	ch := s.app.ProjectManager.Subscribe(r.Context())

	// Send an initial heartbeat so the client knows the stream is live.
	fmt.Fprintf(w, "event: connected\ndata: {\"ts\":%d}\n\n", time.Now().UnixMilli())
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload := event.Payload

			// Build a JSON payload.
			data, err := json.Marshal(map[string]string{
				"project_id": payload.ProjectID,
				"status":     payload.Status,
				"error":      payload.Error,
			})
			if err != nil {
				continue
			}

			// Map ManagerEventType to SSE event name.
			// The frontend listens for "switched", "status_changed", and "init_required".
			var evtName string
			switch payload.Type {
			case project.EvProjectSwitched:
				evtName = "switched"
			case project.EvStatusChanged:
				evtName = "status_changed"
			case project.EvInitRequired:
				evtName = "init_required"
			default:
				evtName = string(payload.Type)
			}

			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evtName, data)
			flusher.Flush()
		}
	}
}
