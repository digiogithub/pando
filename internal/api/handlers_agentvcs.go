package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
			"id":                 cs.ID,
			"short_id":           cs.ShortID,
			"parent_id":          cs.ParentID,
			"session_id":         cs.SessionID,
			"description":        cs.Description,
			"file_count":         cs.FileCount,
			"total_size":         cs.TotalSize,
			"changed_files":      cs.ChangedFiles,
			"changed_total_size": cs.ChangedTotalSize,
			"created_at":         time.Unix(cs.CreatedAt, 0),
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

// handleAgentVCSBlobContent handles GET /api/v1/agentvcs/blobs/{hash}.
// Returns the raw file content for a given blob hash.
func (s *Server) handleAgentVCSBlobContent(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	hash := r.PathValue("hash")
	if hash == "" {
		hash = strings.TrimPrefix(r.URL.Path, "/api/v1/agentvcs/blobs/")
		hash = strings.TrimSuffix(hash, "/")
	}

	content, err := s.app.AgentVCS.GetFileContent(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "blob not found")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}

// handleAgentVCSRevert handles POST /api/v1/agentvcs/commits/{id}/revert.
// Reverts the entire working directory to the state of the given commit.
func (s *Server) handleAgentVCSRevert(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	commitID := r.PathValue("id")
	if commitID == "" {
		commitID = extractPathSegment(r.URL.Path, "/api/v1/agentvcs/commits/", "/revert")
	}

	if err := s.app.AgentVCS.Revert(r.Context(), commitID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reverted"})
}

// revertFilesRequest is the body for POST /api/v1/agentvcs/commits/{id}/revert-files.
type revertFilesRequest struct {
	Files []string `json:"files"`
}

// handleAgentVCSRevertFiles handles POST /api/v1/agentvcs/commits/{id}/revert-files.
// Reverts only the specified files to the state captured in the given commit.
func (s *Server) handleAgentVCSRevertFiles(w http.ResponseWriter, r *http.Request) {
	if s.app.AgentVCS == nil {
		writeError(w, http.StatusServiceUnavailable, "agent-vcs service is disabled")
		return
	}

	commitID := r.PathValue("id")
	if commitID == "" {
		commitID = extractPathSegment(r.URL.Path, "/api/v1/agentvcs/commits/", "/revert-files")
	}

	var req revertFilesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "files list is required")
		return
	}

	commit, err := s.app.AgentVCS.GetCommit(r.Context(), commitID)
	if err != nil {
		writeError(w, http.StatusNotFound, "commit not found")
		return
	}

	tree, err := s.app.AgentVCS.GetTree(r.Context(), commit.TreeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load tree")
		return
	}

	// Index target files by path.
	treeIdx := make(map[string]string, len(tree.Entries))
	for _, e := range tree.Entries {
		if !e.IsDir {
			treeIdx[e.Path] = e.Hash
		}
	}

	workingDir, err := os.Getwd()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get working directory")
		return
	}

	restored := make([]string, 0, len(req.Files))
	for _, filePath := range req.Files {
		hash, ok := treeIdx[filePath]
		if !ok {
			// File doesn't exist in target commit — delete from working dir.
			absPath := filepath.Join(workingDir, filepath.FromSlash(filePath))
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				continue
			}
			restored = append(restored, filePath)
			continue
		}

		content, err := s.app.AgentVCS.GetFileContent(r.Context(), hash)
		if err != nil {
			continue
		}

		absPath := filepath.Join(workingDir, filepath.FromSlash(filePath))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			continue
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			continue
		}
		restored = append(restored, filePath)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "partial_revert",
		"restored": restored,
	})
}

// extractPathSegment extracts a segment between prefix and suffix in a URL path.
func extractPathSegment(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	return s
}
