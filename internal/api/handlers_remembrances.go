package api

import (
	"encoding/json"
	"net/http"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/rag/code"
)

// codeProjectResponse is a slimmed-down representation of a CodeProject for the API.
type codeProjectResponse struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	Status    string `json:"status"`
}

// handleListCodeProjects returns all indexed code projects.
// GET /api/v1/remembrances/projects
func (s *Server) handleListCodeProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.Remembrances == nil || s.app.Remembrances.Code == nil {
		writeJSON(w, http.StatusOK, []codeProjectResponse{})
		return
	}

	projects, err := s.app.Remembrances.Code.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects: "+err.Error())
		return
	}

	resp := make([]codeProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, codeProjectResponse{
			ProjectID: p.ProjectID,
			Name:      p.Name,
			RootPath:  p.RootPath,
			Status:    string(p.IndexingStatus),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// indexProjectRequest is the request body for indexing a new project.
type indexProjectRequest struct {
	// ProjectID to use (optional — defaults to sanitized path basename).
	ProjectID string `json:"project_id,omitempty"`
	// Path to index. Defaults to the working directory if omitted.
	Path string `json:"path,omitempty"`
}

// handleIndexCodeProject starts indexing a code project.
// POST /api/v1/remembrances/projects/index
func (s *Server) handleIndexCodeProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.Remembrances == nil || s.app.Remembrances.Code == nil {
		writeError(w, http.StatusServiceUnavailable, "remembrances code indexer not initialized")
		return
	}

	var req indexProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetPath := req.Path
	if targetPath == "" {
		targetPath = config.WorkingDirectory()
	}

	projectID := req.ProjectID
	if projectID == "" {
		projectID = sanitizeProjectID(targetPath)
	}

	jobID, err := s.app.Remembrances.Code.IndexProject(r.Context(), projectID, targetPath, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start indexing: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"project_id": projectID,
		"job_id":     jobID,
		"path":       targetPath,
		"status":     string(code.IndexingStatusPending),
	})
}

// sanitizeProjectID converts an absolute path to a safe project identifier.
func sanitizeProjectID(path string) string {
	// Use the last path component
	clean := path
	for len(clean) > 0 && (clean[len(clean)-1] == '/' || clean[len(clean)-1] == '\\') {
		clean = clean[:len(clean)-1]
	}
	for i := len(clean) - 1; i >= 0; i-- {
		if clean[i] == '/' || clean[i] == '\\' {
			clean = clean[i+1:]
			break
		}
	}
	if clean == "" {
		clean = "project"
	}
	// Replace characters that are awkward in IDs
	out := make([]byte, 0, len(clean))
	for _, b := range []byte(clean) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			out = append(out, b)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

// enrichmentStatusResponse is the JSON body for enrichment status and toggle endpoints.
type enrichmentStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	PlannerMode string `json:"planner_mode"` // "llm" or "heuristic"
}

// handleGetEnrichmentStatus returns the current context enrichment state.
// GET /api/v1/remembrances/enrichment
func (s *Server) handleGetEnrichmentStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	e := s.app.ContextEnricher
	resp := enrichmentStatusResponse{
		Enabled:     e.IsEnabled(),
		PlannerMode: e.PlannerMode(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleToggleEnrichment enables or disables context enrichment at runtime without restart.
// PUT /api/v1/remembrances/enrichment
// Body: { "enabled": true|false }
func (s *Server) handleToggleEnrichment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app.ContextEnricher == nil {
		writeError(w, http.StatusServiceUnavailable, "context enricher not initialized")
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.app.ContextEnricher.SetEnabled(body.Enabled)

	resp := enrichmentStatusResponse{
		Enabled:     s.app.ContextEnricher.IsEnabled(),
		PlannerMode: s.app.ContextEnricher.PlannerMode(),
	}
	writeJSON(w, http.StatusOK, resp)
}
