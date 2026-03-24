package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/snapshot"
)

// SnapshotResponse is the JSON representation of a snapshot for the web-UI.
type SnapshotResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SessionID   string    `json:"session_id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	WorkingDir  string    `json:"working_dir"`
	CreatedAt   time.Time `json:"created_at"`
	Size        int64     `json:"size"`
	FilesCount  int       `json:"files_count"`
}

// snapshotToResponse maps the internal snapshot.Snapshot to SnapshotResponse.
func snapshotToResponse(s snapshot.Snapshot) SnapshotResponse {
	name := s.Description
	if name == "" {
		name = s.Type + "-" + s.ID[:8]
	}
	return SnapshotResponse{
		ID:         s.ID,
		Name:       name,
		SessionID:  s.SessionID,
		Type:       s.Type,
		Status:     "active",
		WorkingDir: s.WorkingDir,
		CreatedAt:  time.Unix(s.CreatedAt, 0),
		Size:       s.TotalSize,
		FilesCount: s.FileCount,
	}
}

// createSnapshotRequest is the body accepted by POST /api/v1/snapshots.
type createSnapshotRequest struct {
	SessionID   string `json:"session_id"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// handleGetSnapshots handles GET /api/v1/snapshots.
func (s *Server) handleGetSnapshots(w http.ResponseWriter, r *http.Request) {
	if s.app.Snapshots == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"snapshots": []interface{}{}})
		return
	}

	snapshots, err := s.app.Snapshots.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]SnapshotResponse, 0, len(snapshots))
	for _, snap := range snapshots {
		items = append(items, snapshotToResponse(snap))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"snapshots": items})
}

// handleCreateSnapshot handles POST /api/v1/snapshots.
func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.Snapshots == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot service is disabled")
		return
	}

	var req createSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	snapshotType := req.Type
	if snapshotType == "" {
		snapshotType = snapshot.SnapshotTypeManual
	}

	snap, err := s.app.Snapshots.Create(r.Context(), req.SessionID, snapshotType, req.Description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, snapshotToResponse(snap))
}

// handleGetSnapshotByID handles GET /api/v1/snapshots/{id}.
func (s *Server) handleGetSnapshotByID(w http.ResponseWriter, r *http.Request) {
	if s.app.Snapshots == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot service is disabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		// Fallback for older Go versions or non-pattern routes.
		id = strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
		id = strings.TrimSuffix(id, "/")
	}

	snap, err := s.app.Snapshots.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}

	writeJSON(w, http.StatusOK, snapshotToResponse(snap))
}

// applySnapshotRequest is the body accepted by POST /api/v1/snapshots/{id}/apply.
// to_snapshot_id is the target snapshot; the {id} in the path is the "from" snapshot.
type applySnapshotRequest struct {
	ToSnapshotID string `json:"to_snapshot_id"`
}

// handleApplySnapshot handles POST /api/v1/snapshots/{id}/apply.
func (s *Server) handleApplySnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.Snapshots == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot service is disabled")
		return
	}

	id := extractSnapshotIDFromAction(r, "apply")

	var req applySnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ToSnapshotID == "" {
		writeError(w, http.StatusBadRequest, "to_snapshot_id is required")
		return
	}

	if err := s.app.Snapshots.Apply(r.Context(), id, req.ToSnapshotID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// handleRevertSnapshot handles POST /api/v1/snapshots/{id}/revert.
func (s *Server) handleRevertSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.Snapshots == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot service is disabled")
		return
	}

	id := extractSnapshotIDFromAction(r, "revert")

	if err := s.app.Snapshots.Revert(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reverted"})
}

// handleDeleteSnapshot handles DELETE /api/v1/snapshots/{id}.
func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.app.Snapshots == nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot service is disabled")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
		id = strings.TrimSuffix(id, "/")
	}

	if err := s.app.Snapshots.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// extractSnapshotIDFromAction parses the snapshot ID from paths like
// /api/v1/snapshots/{id}/apply or /api/v1/snapshots/{id}/revert.
// It first tries PathValue (Go 1.22+ mux pattern), then falls back to
// manual parsing for compatibility with the prefix-based route fallback.
func extractSnapshotIDFromAction(r *http.Request, action string) string {
	id := r.PathValue("id")
	if id != "" {
		return id
	}
	// Manual parse: strip prefix and suffix action segment.
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
	path = strings.TrimSuffix(path, "/"+action)
	return path
}
