package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
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
	External    bool   `json:"external"` // running but launched by another application
	// Delegations is the count of delegated agent loops running inside the warm
	// instance; DelegationSpawned is true when the instance was auto-started by
	// the delegation router rather than activated by the user.
	Delegations       int    `json:"delegations,omitempty"`
	DelegationSpawned bool   `json:"delegation_spawned,omitempty"`
	ACPPID            int    `json:"acp_pid,omitempty"`
	WebState          string `json:"web_state,omitempty"`
	WebPort           int    `json:"web_port,omitempty"`
	WebURL            string `json:"web_url,omitempty"`
	LastOpened        *int64 `json:"last_opened,omitempty"` // Unix seconds
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

type projectWebInstanceResponse struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	WebPort     int    `json:"web_port"`
	WebURL      string `json:"web_url"`
	PID         int    `json:"pid"`
	State       string `json:"state"`
	StartedAt   string `json:"started_at"`
	Delegations int    `json:"delegations"`
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

// enrichRuntime reconciles the persisted status with the live runtime state.
// It marks instances launched by another application as external and corrects a
// stale "running" status when no live instance is actually serving the path.
func (s *Server) enrichRuntime(resp *projectResponse, p project.Project) {
	mgr := s.projectManagerAPI()
	if mgr == nil {
		return
	}
	running, external, _ := mgr.Runtime(p.ID, p.Path)
	resp.External = external
	switch {
	case running:
		resp.Status = project.StatusRunning
	case resp.Status == project.StatusRunning:
		// DB says running but nothing live serves the path — correct it.
		resp.Status = project.StatusStopped
	}
	// Surface warm-delegation state for manager-owned instances.
	inflight, spawned, _ := mgr.DelegationInfo(p.ID)
	resp.Delegations = inflight
	resp.DelegationSpawned = spawned
	resp.WebState = string(project.WebStateStopped)
	resp.WebURL = projectWebBrowserURL(p.ID)
	if inst, ok := mgr.WebInstance(p.ID); ok {
		resp.WebState = string(inst.State)
		resp.WebPort = inst.Port
	}
}

// handleListProjects handles GET /api/v1/projects.
// Returns all registered projects as {"projects": [...]}.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	projects, err := mgr.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]projectResponse, len(projects))
	for i, p := range projects {
		resp[i] = toProjectResponse(p)
		s.enrichRuntime(&resp[i], p)
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
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
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

	p, err := mgr.Register(r.Context(), req.Name, req.Path)
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
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	if s.projectService() == nil {
		writeError(w, http.StatusServiceUnavailable, "project service not available")
		return
	}
	if s.projectManagerAPI() == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	p, err := s.projectService().Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	resp := toProjectResponse(*p)
	s.enrichRuntime(&resp, *p)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": resp,
	})
}

// handleDeleteProject handles DELETE /api/v1/projects/{id}.
// Returns 204 on success.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	if err := mgr.Unregister(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleActivateProject handles POST /api/v1/projects/{id}/activate.
// It starts or focuses the ACP delegation child for the project; opening the
// project's WebUI is a separate operation handled by /web/open.
// Returns 409 Conflict with {"error":"project_needs_init","project_id":"...","path":"..."}
// when the project directory has no Pando configuration file.
func (s *Server) handleActivateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	err := mgr.Activate(r.Context(), id)
	if err != nil {
		if errors.Is(err, project.ErrProjectNeedsInit) {
			// Retrieve path for the response body.
			var projPath string
			if svc := s.projectService(); svc != nil {
				if p, getErr := svc.Get(r.Context(), id); getErr == nil {
					projPath = p.Path
				}
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
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	if err := mgr.Deactivate(r.Context()); err != nil {
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
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	if err := mgr.CompleteInit(r.Context(), id); err != nil {
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
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	p, err := mgr.ActiveProject(r.Context())
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
	s.enrichRuntime(&resp, *p)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": resp,
	})
}

// handleStopProject handles POST /api/v1/projects/{id}/stop.
// It stops both manager-owned project child types for the project: the ACP
// delegation child and the background WebUI child, if present.
// Returns 409 Conflict with {"error":"external_instance","project_id":"..."}
// when the instance was launched by another application and cannot be stopped.
func (s *Server) handleStopProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	cancelled, err := mgr.StopReport(r.Context(), id)
	if err != nil {
		if errors.Is(err, project.ErrExternalInstance) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":      "external_instance",
				"project_id": id,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// cancelled_delegations lets the UI warn the user how many delegated agent
	// loops were interrupted by the stop (they fall back to the cold path).
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":                "stopped",
		"project_id":            id,
		"cancelled_delegations": cancelled,
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
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
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

	if err := mgr.Rename(r.Context(), id, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	p, err := s.projectService().Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project": toProjectResponse(*p),
	})
}

// handleOpenProjectWeb handles POST /api/v1/projects/{id}/web/open.
// It starts or reuses the background project WebUI child. This is distinct
// from activate, which manages the ACP delegation child, and from
// open-desktop, which opens a separate native desktop window.
func (s *Server) handleOpenProjectWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	svc := s.projectService()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "project service not available")
		return
	}

	id := r.PathValue("id")
	proj, err := svc.Get(r.Context(), id)
	if err != nil || proj == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	status := "opened"
	if existing, ok := mgr.WebInstance(id); ok {
		switch existing.State {
		case project.WebStateStarting, project.WebStateRunning:
			status = "already_open"
		}
	}

	inst, err := mgr.OpenWeb(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, project.ErrProjectNeedsInit):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":      "project_needs_init",
				"project_id": id,
				"path":       proj.Path,
			})
			return
		case errors.Is(err, project.ErrChildInstance):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "child_instance"})
			return
		case errors.Is(err, project.ErrDelegationsInFlight):
			count := 0
			var inflightErr *project.DelegationsInFlightError
			if errors.As(err, &inflightErr) && inflightErr != nil {
				count = inflightErr.Count
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":       "delegations_in_flight",
				"project_id":  id,
				"delegations": count,
			})
			return
		default:
			var startupErr *project.ChildStartupError
			if errors.As(err, &startupErr) || errors.Is(err, project.ErrChildStartupFailed) || errors.Is(err, project.ErrChildStartupTimeout) {
				detail := err.Error()
				if startupErr != nil && startupErr.Detail != "" {
					detail = startupErr.Detail
				}
				writeJSON(w, http.StatusBadGateway, map[string]string{
					"error":  "child_startup_failed",
					"detail": detail,
				})
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.setProjectWebCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     status,
		"project_id": id,
		"web_url":    projectWebBrowserURL(id),
		"web_port":   inst.Port,
	})
}

// handleCloseProjectWeb handles POST /api/v1/projects/{id}/web/close.
// It stops only the background project WebUI child; use /stop to stop both the
// WebUI child and the ACP delegation child.
func (s *Server) handleCloseProjectWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	id := r.PathValue("id")
	cancelled, _, _ := mgr.DelegationInfo(id)
	if err := mgr.CloseWeb(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":                "closed",
		"project_id":            id,
		"cancelled_delegations": cancelled,
	})
}

// handleListProjectWebInstances handles GET /api/v1/projects/web.
func (s *Server) handleListProjectWebInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}
	// Restoring tabs after a reload (or a parent restart, which mints a new
	// token) must refresh the cookie the frames load with.
	s.setProjectWebCookie(w, r)

	mgr := s.projectManagerAPI()
	if mgr == nil {
		writeError(w, http.StatusServiceUnavailable, "project manager not available")
		return
	}

	instances := mgr.WebInstances()
	resp := make([]projectWebInstanceResponse, 0, len(instances))
	for _, inst := range instances {
		delegations, _, _ := mgr.DelegationInfo(inst.Project.ID)
		resp = append(resp, projectWebInstanceResponse{
			ProjectID:   inst.Project.ID,
			Name:        inst.Project.Name,
			Path:        inst.Project.Path,
			WebPort:     inst.Port,
			WebURL:      projectWebBrowserURL(inst.Project.ID),
			PID:         inst.PID,
			State:       string(inst.State),
			StartedAt:   inst.StartedAt.UTC().Format(time.RFC3339),
			Delegations: delegations,
		})
	}
	sort.Slice(resp, func(i, j int) bool {
		if resp[i].StartedAt != resp[j].StartedAt {
			return resp[i].StartedAt < resp[j].StartedAt
		}
		return resp[i].ProjectID < resp[j].ProjectID
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"instances": resp})
}

// handleProjectEvents handles GET /api/v1/projects/events.
// It streams Server-Sent Events for project lifecycle changes.
func (s *Server) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}

	mgr := s.projectManagerAPI()
	if mgr == nil {
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
	ch := mgr.Subscribe(r.Context())

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
			data, err := json.Marshal(map[string]interface{}{
				"project_id":  payload.ProjectID,
				"status":      payload.Status,
				"error":       payload.Error,
				"delegations": payload.Count,
				"web_port":    payload.Port,
			})
			if err != nil {
				continue
			}

			// Map ManagerEventType to SSE event name. The frontend listens for
			// "switched", "status_changed", "init_required" and "delegation_changed".
			var evtName string
			switch payload.Type {
			case project.EvProjectSwitched:
				evtName = "switched"
			case project.EvStatusChanged:
				evtName = "status_changed"
			case project.EvInitRequired:
				evtName = "init_required"
			case project.EvDelegationChanged:
				evtName = "delegation_changed"
			case project.EvWebStarted:
				evtName = "web_started"
			case project.EvWebStopped:
				evtName = "web_stopped"
			case project.EvWebError:
				evtName = "web_error"
			default:
				evtName = string(payload.Type)
			}

			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evtName, data)
			flusher.Flush()
		}
	}
}
