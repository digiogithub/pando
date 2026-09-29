package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/digiogithub/pando/internal/config"
)

// uiPrefsFileName is the user-level file holding WebUI preferences that must
// survive a restart regardless of the origin the UI is served from. Browser
// storage is per origin, and the desktop app or a project instance may land on
// a different port on every launch, so localStorage alone loses them.
const uiPrefsFileName = "webui-prefs.json"

// UIPrefs are the WebUI preferences persisted on the user's machine.
type UIPrefs struct {
	// ChatMode is "simple" or "advanced"; empty means "never chosen".
	ChatMode string `json:"chatMode"`
}

var uiPrefsMu sync.Mutex

func uiPrefsPath() string {
	dir := config.GlobalConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, uiPrefsFileName)
}

func loadUIPrefs() (UIPrefs, error) {
	var prefs UIPrefs
	path := uiPrefsPath()
	if path == "" {
		return prefs, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		// A corrupt file is not worth failing the UI over: start fresh.
		return UIPrefs{}, nil
	}
	return prefs, nil
}

func saveUIPrefs(prefs UIPrefs) error {
	path := uiPrefsPath()
	if path == "" {
		return errors.New("no user config directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// handleUIPrefs serves GET (read) and PUT (merge) of the WebUI preferences.
func (s *Server) handleUIPrefs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		uiPrefsMu.Lock()
		prefs, err := loadUIPrefs()
		uiPrefsMu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, prefs)
	case http.MethodPut:
		var patch UIPrefs
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&patch); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if patch.ChatMode != "" && patch.ChatMode != "simple" && patch.ChatMode != "advanced" {
			writeError(w, http.StatusBadRequest, "chatMode must be \"simple\" or \"advanced\"")
			return
		}
		uiPrefsMu.Lock()
		defer uiPrefsMu.Unlock()
		prefs, err := loadUIPrefs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if patch.ChatMode != "" {
			prefs.ChatMode = patch.ChatMode
		}
		if err := saveUIPrefs(prefs); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, prefs)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
