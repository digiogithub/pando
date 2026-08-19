package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
)

// firstUserPrompt returns the text of the first user message of a session, or
// "" when there is none. It is used to derive display fallbacks for sessions
// that still carry a placeholder title.
func (s *Server) firstUserPrompt(ctx context.Context, sessionID string) string {
	msgs, err := s.app.Messages.List(ctx, sessionID)
	if err != nil {
		return ""
	}
	for _, msg := range msgs {
		if msg.Role == message.User {
			if tc, ok := firstTextPart(msg.Parts); ok {
				return tc
			}
		}
	}
	return ""
}

func firstTextPart(parts []message.ContentPart) (string, bool) {
	for _, part := range parts {
		if tc, ok := part.(message.TextContent); ok {
			return tc.Text, true
		}
	}
	return "", false
}

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

	// Paginate: sessions come ordered by updated_at DESC, so a window over the
	// slice is the newest-first page the UI asks for. Clients that send no
	// parameters get the first page instead of every session ever created.
	limit, offset := paginationParams(r, defaultSessionPageSize)
	total := len(sessions)
	page := paginate(sessions, limit, offset)

	type sessionWithStatus struct {
		session.Session
		IsRunning     bool   `json:"is_running"`
		PromptPreview string `json:"prompt_preview,omitempty"`
	}
	result := make([]sessionWithStatus, len(page))
	for i, sess := range page {
		result[i] = sessionWithStatus{
			Session:   sess,
			IsRunning: s.bgRunner.IsBusy(sess.ID),
		}
		// Sessions that still carry a placeholder title (or a delegated marker
		// without a prompt snippet) get a display title derived from their
		// first user prompt, plus a prompt_preview the WebUI shows on hover.
		prompt := ""
		if session.IsDefaultTitle(sess.Title) || strings.HasPrefix(sess.Title, "delegated: ") {
			prompt = s.firstUserPrompt(r.Context(), sess.ID)
		}
		if snippet := session.TitleFromPrompt(prompt); snippet != "" {
			result[i].PromptPreview = snippet
			switch {
			case session.IsDefaultTitle(sess.Title):
				result[i].Title = snippet
			case strings.HasPrefix(sess.Title, "delegated: ") && !strings.Contains(sess.Title, "—"):
				result[i].Title = sess.Title + " — " + snippet
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": result,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(result) < total,
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
		"session":    sess,
		"messages":   messages,
		"is_running": s.bgRunner.IsBusy(id),
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
