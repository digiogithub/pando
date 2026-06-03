package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/agentvcs"
)

// SnapshotResponse is the JSON representation of a commit for the web-UI.
// Field names preserve backward compatibility with the old snapshot API.
type SnapshotResponse struct {
	ID         string    `json:"id"`
	ShortID    string    `json:"short_id"`
	Name       string    `json:"name"`
	SessionID  string    `json:"session_id"`
	ParentID   string    `json:"parent_id,omitempty"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	Size       int64     `json:"size"`
	FilesCount int       `json:"files_count"`
}

// commitToResponse maps an agentvcs.Commit to the API response format.
func commitToResponse(c agentvcs.Commit) SnapshotResponse {
	name := c.Description
	if name == "" {
		sid := c.ID
		if len(sid) > 8 {
			sid = sid[:8]
		}
		name = "commit-" + sid
	}
	commitType := "delta"
	if c.ParentID == "" {
		commitType = "start"
	}
	shortID := c.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	return SnapshotResponse{
		ID:         c.ID,
		ShortID:    shortID,
		Name:       name,
		SessionID:  c.SessionID,
		ParentID:   c.ParentID,
		Type:       commitType,
		Status:     "active",
		CreatedAt:  time.Unix(c.CreatedAt, 0),
		Size:       c.TotalSize,
		FilesCount: c.FileCount,
	}
}

// createSnapshotRequest is the body accepted by POST /api/v1/snapshots.
type createSnapshotRequest struct {
	SessionID   string `json:"session_id"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// handleGetSnapshots handles GET /api/v1/snapshots.
// When a ?session_id query param is present, returns only that session's commits.
func (s *Server) handleGetSnapshots(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"snapshots": []interface{}{}})
		return
	}

	sessionID := r.URL.Query().Get("session_id")

	if sessionID != "" {
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
		return
	}

	// List all sessions and their latest commits.
	sessions, err := s.app.AgentVCS.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var items []SnapshotResponse
	for _, sid := range sessions {
		log, err := s.app.AgentVCS.Log(r.Context(), sid)
		if err != nil || len(log) == 0 {
			continue
		}
		// Return the latest commit per session for the overview.
		latest := log[len(log)-1]
		c, err := s.app.AgentVCS.GetCommit(r.Context(), latest.ID)
		if err != nil {
			continue
		}
		items = append(items, commitToResponse(c))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"snapshots": items})
}

// handleCreateSnapshot handles POST /api/v1/snapshots.
func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	var req createSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	desc := req.Description
	if desc == "" {
		desc = "Manual commit"
	}

	commit, err := s.app.AgentVCS.Record(r.Context(), req.SessionID, desc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, commitToResponse(commit))
}

// handleGetSnapshotByID handles GET /api/v1/snapshots/{id}.
func (s *Server) handleGetSnapshotByID(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
		id = strings.TrimSuffix(id, "/")
	}

	commit, err := s.app.AgentVCS.GetCommit(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "commit not found")
		return
	}

	// Include diff from parent.
	diffs, _ := s.app.AgentVCS.DiffFromParent(r.Context(), id)

	resp := map[string]interface{}{
		"commit": commitToResponse(commit),
		"diff":   diffs,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleApplySnapshot handles POST /api/v1/snapshots/{id}/apply.
// In agent-vcs this is a no-op placeholder — use revert instead.
func (s *Server) handleApplySnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}
	writeError(w, http.StatusNotImplemented, "use revert endpoint to restore a commit state")
}

// handleRevertSnapshot handles POST /api/v1/snapshots/{id}/revert.
func (s *Server) handleRevertSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	id := extractSnapshotIDFromAction(r, "revert")

	if err := s.app.AgentVCS.Revert(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reverted"})
}

// handleDeleteSnapshot handles DELETE /api/v1/snapshots/{id}.
// In agent-vcs, individual commit deletion is not supported (commits are immutable).
func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}
	writeError(w, http.StatusNotImplemented, "commits are immutable; use cleanup to prune old sessions")
}

// extractSnapshotIDFromAction parses the snapshot ID from paths like
// /api/v1/snapshots/{id}/apply or /api/v1/snapshots/{id}/revert.
func extractSnapshotIDFromAction(r *http.Request, action string) string {
	id := r.PathValue("id")
	if id != "" {
		return id
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
	path = strings.TrimSuffix(path, "/"+action)
	return path
}
