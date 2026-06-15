package api

import (
	"encoding/json"
	"net/http"

	"github.com/digiogithub/pando/internal/permission"
)

// permissionRespondRequest is the body of POST /api/v1/permissions/respond.
type permissionRespondRequest struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	// Action is one of "allow", "allow_session" or "deny".
	Action string `json:"action"`
}

// handlePermissionRespond resolves a pending tool permission request that was
// surfaced to the Web UI via a permission_request SSE event.
func (s *Server) handlePermissionRespond(w http.ResponseWriter, r *http.Request) {
	var req permissionRespondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	// Reconstruct the full request from the pending set so allow_session can
	// persist tool/action/path. Fall back to a minimal request if not found
	// (the channel response only needs the ID).
	perm := permission.PermissionRequest{ID: req.ID, SessionID: req.SessionID}
	for _, p := range s.app.Permissions.PendingRequests(req.SessionID) {
		if p.ID == req.ID {
			perm = p
			break
		}
	}

	switch req.Action {
	case "allow":
		s.app.Permissions.Grant(perm)
	case "allow_session":
		s.app.Permissions.GrantPersistant(perm)
	case "deny":
		s.app.Permissions.Deny(perm)
	default:
		writeError(w, http.StatusBadRequest, "action must be one of allow, allow_session, deny")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type autoApproveResponse struct {
	SessionID string `json:"sessionId"`
	Enabled   bool   `json:"enabled"`
}

// handleGetAutoApprove reports whether a session is currently in auto-approve mode.
func (s *Server) handleGetAutoApprove(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}
	writeJSON(w, http.StatusOK, autoApproveResponse{
		SessionID: sessionID,
		Enabled:   s.app.Permissions.IsAutoApproveSession(sessionID),
	})
}

// handleSetAutoApprove toggles per-session auto-approve ("auto mode").
func (s *Server) handleSetAutoApprove(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Mark as seeded so the config default never overrides this explicit choice.
	s.seededSessions.Store(sessionID, true)
	if body.Enabled {
		s.app.Permissions.AutoApproveSession(sessionID)
	} else {
		s.app.Permissions.RemoveAutoApproveSession(sessionID)
	}
	writeJSON(w, http.StatusOK, autoApproveResponse{
		SessionID: sessionID,
		Enabled:   body.Enabled,
	})
}
