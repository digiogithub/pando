package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// handleSnapshotsCount handles GET /api/v1/snapshots/count.
// Returns the number of snapshots currently stored.
func (s *Server) handleSnapshotsCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.app.Snapshots == nil {
		writeJSON(w, http.StatusOK, map[string]int{"count": 0})
		return
	}

	snapshots, err := s.app.Snapshots.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list snapshots: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{"count": len(snapshots)})
}

// handleRegenerateAPIToken handles POST /api/v1/config/api-server/regenerate-token.
// Generates a new server auth token and returns it.
func (s *Server) handleRegenerateAPIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	newToken := hex.EncodeToString(b)
	s.token = newToken

	writeJSON(w, http.StatusOK, map[string]string{"token": newToken})
}

// handleSkillsCatalog handles GET /api/v1/skills/catalog.
// Returns an empty catalog — catalog browsing requires a remote endpoint
// that is not yet configured. The web-UI renders gracefully with an empty list.
func (s *Server) handleSkillsCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"skills": []interface{}{}})
}

// handleInstallSkill handles POST /api/v1/skills/install.
func (s *Server) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeError(w, http.StatusNotImplemented, "skill installation from catalog is not yet supported")
}

// handleUninstallSkill handles DELETE /api/v1/skills/{name}.
func (s *Server) handleUninstallSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeError(w, http.StatusNotImplemented, "skill uninstall is not yet supported via API")
}
