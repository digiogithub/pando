package api

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/updatecheck"
)

type serverInfoResponse struct {
	Version          string `json:"version"`
	StartupMode      string `json:"startup_mode"`
	ParentInstanceID string `json:"parent_instance_id"`
	ProjectID        string `json:"project_id"`
	ProjectName      string `json:"project_name"`
	PublicBasePath   string `json:"public_base_path"`
}

type healthResponse struct {
	Status string `json:"status"`
	serverInfoResponse
	// PID is reported in project-child mode only, so the parent can verify the
	// process it started is the one answering on the port.
	PID int `json:"pid,omitempty"`
}

func (s *Server) serverInfoPayload() serverInfoResponse {
	return serverInfoResponse{
		Version:          s.config.Version,
		StartupMode:      s.config.StartupMode,
		ParentInstanceID: s.config.ParentInstanceID,
		ProjectID:        s.config.ProjectID,
		ProjectName:      s.config.ProjectName,
		PublicBasePath:   s.config.PublicBasePath,
	}
}

func (s *Server) isProjectChildMode() bool {
	return strings.EqualFold(s.config.StartupMode, "project-child")
}

func (s *Server) writeChildModeUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "not_available_in_child"})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:             "healthy",
		serverInfoResponse: s.serverInfoPayload(),
	}
	if s.isProjectChildMode() {
		resp.PID = os.Getpid()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"token": s.token,
	})
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cwd":     s.config.CWD,
		"version": s.config.Version,
	})
}

func (s *Server) handleProjectContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	context := map[string]interface{}{
		"cwd":     s.config.CWD,
		"version": s.config.Version,
	}

	writeJSON(w, http.StatusOK, context)
}

// handleVersion reports the running Pando version and whether a newer release
// can be installed with `pando update`. The WebUI shows it but never downloads.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, updatecheck.CurrentStatus(context.WithoutCancel(r.Context())))
}
