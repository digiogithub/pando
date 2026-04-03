package api

import (
	"encoding/json"
	"net/http"

	agentpkg "github.com/digiogithub/pando/internal/llm/agent"
)

// handleListPersonas handles GET /api/v1/personas.
// Returns all available persona names loaded by the persona selector.
func (s *Server) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	personas := agentpkg.ListAvailablePersonas()
	if personas == nil {
		personas = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"personas": personas,
	})
}

// handleGetActivePersona handles GET /api/v1/personas/active.
// Returns the currently active persona name (empty string if none is active).
func (s *Server) handleGetActivePersona(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"active": agentpkg.GetActivePersona(),
	})
}

// handleSetActivePersona handles PUT /api/v1/personas/active.
// Accepts {"name": "persona-name"} to activate a persona, or {"name": ""} to clear it.
func (s *Server) handleSetActivePersona(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: expected JSON with 'name' field")
		return
	}

	if err := agentpkg.SetActivePersona(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"active": req.Name,
	})
}
