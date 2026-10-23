package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/history"
)

// handleSessionFileOriginal handles GET /api/v1/sessions/{id}/file-original?path=...
// It returns the content a file had when the session first touched it, taken
// from the per-session file history the edit/write/patch tools record. The diff
// viewer uses it as the left pane when agent-vcs has no data for the file: a
// `write` replaces the whole file, so the original cannot be rebuilt from the
// tool input alone.
func (s *Server) handleSessionFileOriginal(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	path := r.URL.Query().Get("path")
	if sessionID == "" || path == "" {
		writeError(w, http.StatusBadRequest, "session id and path are required")
		return
	}
	if s.app.History == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"found": false})
		return
	}

	files, err := s.app.History.ListBySession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Versions come oldest first; the "initial" one is the state before the
	// session's first change, and the first match is the fallback when it is
	// missing.
	var original *history.File
	for i := range files {
		f := &files[i]
		if !sameHistoryPath(f.Path, path) {
			continue
		}
		if f.Version == history.InitialVersion {
			original = f
			break
		}
		if original == nil {
			original = f
		}
	}
	if original == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"found": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"found":    true,
		"path":     original.Path,
		"version":  original.Version,
		"original": original.Content,
	})
}

// sameHistoryPath reports whether a recorded history path names the requested
// file. Tools record absolute paths, while the UI may hold a path relative to
// the workspace root.
func sameHistoryPath(recorded, requested string) bool {
	recorded = filepath.ToSlash(filepath.Clean(recorded))
	requested = filepath.ToSlash(filepath.Clean(requested))
	if recorded == requested {
		return true
	}
	if strings.HasPrefix(requested, "/") {
		return false
	}
	return strings.HasSuffix(recorded, "/"+requested)
}
