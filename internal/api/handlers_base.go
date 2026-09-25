package api

import (
	"context"
	"net/http"

	"github.com/digiogithub/pando/internal/updatecheck"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "healthy",
		"version": s.config.Version,
	})
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
