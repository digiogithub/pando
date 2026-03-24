package api

import (
	"encoding/json"
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
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetSession(w, r, path)
	case http.MethodDelete:
		s.handleDeleteSession(w, r, path)
	case http.MethodPatch:
		s.handlePatchSession(w, r, path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request, id string) {
	sess, err := s.app.Sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	messages, err := s.app.Messages.List(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":  sess,
		"messages": messages,
	})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request, id string) {
	// Delete all messages first, then the session.
	if err := s.app.Messages.DeleteSessionMessages(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session messages: "+err.Error())
		return
	}

	if err := s.app.Sessions.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// sessionPatchRequest is the body accepted by PATCH /api/v1/sessions/{id}.
type sessionPatchRequest struct {
	Title *string `json:"title,omitempty"`
}

func (s *Server) handlePatchSession(w http.ResponseWriter, r *http.Request, id string) {
	var req sessionPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	sess, err := s.app.Sessions.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if req.Title != nil {
		sess.Title = *req.Title
	}

	updated, err := s.app.Sessions.Save(r.Context(), sess)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update session: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": updated,
	})
}
