package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/desktop"
	"github.com/digiogithub/pando/internal/instanceregistry"
)

// spawnDesktopInstance starts an independent `pando desktop` for a directory.
// It is a variable so tests can observe the launch without opening a window.
var spawnDesktopInstance = desktop.SpawnInstance

// liveDesktopForPath reports whether a desktop instance already serves path.
var liveDesktopForPath = func(path string) bool {
	entries, err := instanceregistry.New().ListByPath(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Mode == instanceregistry.ModeDesktop {
			return true
		}
	}
	return false
}

// handleOpenProjectDesktop handles POST /api/v1/projects/{id}/open-desktop.
// It launches a separate Pando desktop window working in the project's folder.
// Unlike /web/open, it does not start the background project WebUI child or
// affect the ACP delegation child.
// Only a desktop-mode server may do this: spawning native windows is a local
// GUI action that makes no sense for a headless or remotely reached server.
func (s *Server) handleOpenProjectDesktop(w http.ResponseWriter, r *http.Request) {
	if s.isProjectChildMode() {
		s.writeChildModeUnavailable(w)
		return
	}
	if !strings.EqualFold(s.config.StartupMode, "desktop") {
		writeError(w, http.StatusConflict, "opening a desktop window is only available in the desktop app")
		return
	}
	if s.app.Projects == nil {
		writeError(w, http.StatusServiceUnavailable, "project service not available")
		return
	}

	id := r.PathValue("id")
	p, err := s.app.Projects.Get(r.Context(), id)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	path := filepath.Clean(p.Path)
	if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
		writeError(w, http.StatusNotFound, "project folder does not exist: "+path)
		return
	}

	// The current window already works in this folder.
	if filepath.Clean(s.config.CWD) == path {
		writeJSON(w, http.StatusOK, map[string]string{"status": "current", "project_id": id})
		return
	}
	if liveDesktopForPath(path) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_open", "project_id": id})
		return
	}

	if err := spawnDesktopInstance(path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "opened", "project_id": id})
}
