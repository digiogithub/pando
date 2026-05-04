package api

import (
	"net/http"

	"github.com/digiogithub/pando/internal/llm/tools"
)

type BrowserInstallInfo struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Executable  string `json:"executable"`
	UserDataDir string `json:"userDataDir,omitempty"`
}

func (s *Server) handleConfigBrowsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	installs := tools.DetectInstalledBrowsers()
	items := make([]BrowserInstallInfo, 0, len(installs))
	for _, install := range installs {
		items = append(items, BrowserInstallInfo{
			Type:        install.Type,
			Label:       install.Label,
			Executable:  install.Executable,
			UserDataDir: install.UserDataDir,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"browsers": items})
}
