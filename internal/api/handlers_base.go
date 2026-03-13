package api

import (
	"net/http"
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
