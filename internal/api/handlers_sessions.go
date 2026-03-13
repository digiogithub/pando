package api

import (
	"net/http"
	"strings"
)

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessions, err := s.app.Sessions.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
	})
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	sess, err := s.app.Sessions.Get(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	messages, err := s.app.Messages.List(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := map[string]interface{}{
		"session":  sess,
		"messages": messages,
	}

	writeJSON(w, http.StatusOK, response)
}
