package api

import (
	"net/http"
	"strings"
	"time"
)

// handleAgentVCSLog handles GET /api/v1/agentvcs/sessions/{id}/log.
// Returns the ordered commit chain for a session.
func (s *Server) handleAgentVCSLog(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"commits": []interface{}{}})
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		sessionID = extractPathSegment(r.URL.Path, "/api/v1/agentvcs/sessions/", "/log")
	}

	log, err := s.app.AgentVCS.Log(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, 0, len(log))
	for _, cs := range log {
		items = append(items, map[string]interface{}{
			"id":            cs.ID,
			"short_id":      cs.ShortID,
			"parent_id":     cs.ParentID,
			"session_id":    cs.SessionID,
			"description":   cs.Description,
			"file_count":    cs.FileCount,
			"total_size":    cs.TotalSize,
			"changed_files": cs.ChangedFiles,
			"created_at":    time.Unix(cs.CreatedAt, 0),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"commits": items})
}

// handleAgentVCSSessions handles GET /api/v1/agentvcs/sessions.
// Returns the list of session IDs with commit history.
func (s *Server) handleAgentVCSSessions(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": []interface{}{}})
		return
	}

	sessions, err := s.app.AgentVCS.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, 0, len(sessions))
	for _, sid := range sessions {
		count, _ := s.app.AgentVCS.SessionCommitCount(r.Context(), sid)
		items = append(items, map[string]interface{}{
			"session_id":   sid,
			"commit_count": count,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": items})
}

// handleAgentVCSDiff handles GET /api/v1/agentvcs/commits/{id}/diff.
// Returns the file-level changes from the commit's parent.
func (s *Server) handleAgentVCSDiff(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	commitID := r.PathValue("id")
	if commitID == "" {
		commitID = extractPathSegment(r.URL.Path, "/api/v1/agentvcs/commits/", "/diff")
	}

	diffs, err := s.app.AgentVCS.DiffFromParent(r.Context(), commitID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"diff": diffs})
}

// handleAgentVCSCommit handles GET /api/v1/agentvcs/commits/{id}.
func (s *Server) handleAgentVCSCommit(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	commitID := r.PathValue("id")
	if commitID == "" {
		commitID = strings.TrimPrefix(r.URL.Path, "/api/v1/agentvcs/commits/")
		commitID = strings.TrimSuffix(commitID, "/")
	}

	commit, err := s.app.AgentVCS.GetCommit(r.Context(), commitID)
	if err != nil {
		writeError(w, http.StatusNotFound, "commit not found")
		return
	}

	diffs, _ := s.app.AgentVCS.DiffFromParent(r.Context(), commitID)

	resp := map[string]interface{}{
		"commit": commitToResponse(commit),
		"diff":   diffs,
	}
	writeJSON(w, http.StatusOK, resp)
}

// extractPathSegment extracts a segment between prefix and suffix in a URL path.
func extractPathSegment(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	return s
}
