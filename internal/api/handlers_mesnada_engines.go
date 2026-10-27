package api

import (
	"net/http"

	"github.com/digiogithub/pando/internal/mesnada/engines"
)

// MesnadaEnginesResponse is the body of GET /api/v1/mesnada/engines.
type MesnadaEnginesResponse struct {
	Engines []engines.Status `json:"engines"`
}

// handleGetMesnadaEngines lists every subagent engine Mesnada knows about (the
// built-in catalogue plus custom template engines) with whether its binary is
// installed on PATH and whether it is enabled in the Mesnada settings. Native
// Pando engines are reported locked: they are always enabled. Toggles are
// persisted through PUT /api/v1/settings (orchestrator_disabled_engines).
// GET /api/v1/mesnada/engines
func (s *Server) handleGetMesnadaEngines(w http.ResponseWriter, r *http.Request) {
	var custom []engines.CustomEngine
	if s.app != nil && s.app.MesnadaOrchestrator != nil {
		for _, info := range s.app.MesnadaOrchestrator.CustomEngineInfos() {
			custom = append(custom, engines.CustomEngine{Name: info.Name, Command: info.Command})
		}
	}
	writeJSON(w, http.StatusOK, MesnadaEnginesResponse{Engines: engines.Detect(custom, engines.Disabled())})
}
