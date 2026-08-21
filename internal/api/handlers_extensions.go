package api

import (
	"net/http"

	"github.com/digiogithub/pando/internal/extensions"
)

// UIManifestResponse is what the WebUI shell fetches at boot to discover the
// panels this build contributes.
//
// Not to be confused with /api/v1/config/extensions, which is the settings
// screen for skills and Lua hooks. This one describes compiled-in extension
// modules (pkg/extension).
type UIManifestResponse struct {
	// Panels is never nil in the response, so the shell can iterate without a
	// null check on the standard build, where it is simply empty.
	Panels []extensions.Panel `json:"panels"`
}

func (s *Server) handleExtensionsUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	panels := []extensions.Panel{}
	if s.app != nil {
		panels = append(panels, extensions.Panels(s.app.Extensions)...)
	}
	writeJSON(w, http.StatusOK, UIManifestResponse{Panels: panels})
}
