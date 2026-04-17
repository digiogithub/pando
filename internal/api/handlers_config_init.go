package api

import (
	"net/http"

	"github.com/digiogithub/pando/internal/config"
)

// handleConfigInitStatus handles GET /api/v1/config/init-status.
// Returns whether a local .pando.toml exists in the working directory and
// whether the .pando directory has been created (pre-initialised).
func (s *Server) handleConfigInitStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hasLocalConfig": config.HasLocalConfigFile(),
		"hasPandoDir":    config.HasPandoDirectory(),
		"shouldGenerate": config.ShouldGenerateLocalConfig(),
	})
}

// handleConfigGenerate handles POST /api/v1/config/generate.
// Writes the default annotated .pando.toml into the current working directory.
// Returns 409 Conflict if the file already exists.
func (s *Server) handleConfigGenerate(w http.ResponseWriter, r *http.Request) {
	if !config.ShouldGenerateLocalConfig() {
		writeError(w, http.StatusConflict, "local config file already exists")
		return
	}

	if err := config.GenerateLocalConfigFile(config.DefaultConfigTemplate); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"path":    ".pando.toml",
		"message": "Config file generated. Configure your providers and models in Settings.",
	})
}
