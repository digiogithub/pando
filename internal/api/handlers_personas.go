package api

import (
	"encoding/json"
	"net/http"

	"github.com/digiogithub/pando/internal/config"
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
//
// Additive fields describe persona auto-selection: auto is true when no persona
// is selected manually and auto-select is enabled; decisionModel reports the
// persona-selector "use decision model" option; with an optional sessionId
// query parameter, applied and source name the persona auto-selection applied
// to that session on its last turn (empty before the first turn).
func (s *Server) handleGetActivePersona(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	active := agentpkg.GetActivePersona()
	auto := false
	if cfg := config.Get(); cfg != nil {
		auto = active == "" && cfg.PersonaAutoSelect.Enabled
	}
	applied, source := "", ""
	if auto {
		applied, source = agentpkg.AppliedAutoPersona(r.URL.Query().Get("sessionId"))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":        active,
		"auto":          auto,
		"decisionModel": auto && config.PersonaSelectorUsesDecisionModel(),
		"applied":       applied,
		"source":        source,
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
	if err := config.UpdateActivePersona(req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "persona activated but not saved: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"active": req.Name,
	})
}
