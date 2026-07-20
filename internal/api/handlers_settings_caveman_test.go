package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// withCavemanSettings points the config at a throwaway working directory with
// its own .pando.toml, so UpdateCaveman writes there instead of the developer's
// real ~/.pando.toml (ResolveConfigFilePath prefers the local file).
func withCavemanSettings(t *testing.T, defaultMode string) (*Server, string) {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, ".pando.toml")
	if err := os.WriteFile(file, []byte("Debug = true\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	prev := config.Get()
	config.SetForTests(&config.Config{
		WorkingDir: dir,
		Debug:      true,
		Caveman:    config.CavemanConfig{DefaultMode: defaultMode},
	})
	t.Cleanup(func() { config.SetForTests(prev) })

	return &Server{}, file
}

func putSettings(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	return rec
}

func decodeSettings(t *testing.T, rec *httptest.ResponseRecorder) SettingsResponse {
	t.Helper()
	var resp SettingsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	return resp
}

func TestGetSettingsExposesCavemanDefaultMode(t *testing.T) {
	server, _ := withCavemanSettings(t, "lite")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := decodeSettings(t, rec).CavemanDefaultMode; got != "lite" {
		t.Errorf("caveman_default_mode = %q, want %q", got, "lite")
	}
}

func TestPutSettingsPersistsCavemanDefaultMode(t *testing.T) {
	server, file := withCavemanSettings(t, "")

	rec := putSettings(t, server, `{"caveman_default_mode":"ultra"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := decodeSettings(t, rec).CavemanDefaultMode; got != "ultra" {
		t.Errorf("response caveman_default_mode = %q, want %q", got, "ultra")
	}
	if got := config.Get().CavemanDefaultMode(); got != "ultra" {
		t.Errorf("in-memory default = %q, want %q", got, "ultra")
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}
	if !strings.Contains(string(data), "ultra") {
		t.Errorf("expected the level to be persisted, got:\n%s", data)
	}
}

// An empty string is how "no default" is stored, so the Web UI can send it back
// verbatim after loading an off configuration.
func TestPutSettingsCavemanOffClearsDefault(t *testing.T) {
	for _, value := range []string{"off", ""} {
		t.Run("value="+value, func(t *testing.T) {
			server, _ := withCavemanSettings(t, "full")

			body, err := json.Marshal(SettingsUpdateRequest{CavemanDefaultMode: &value})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			rec := putSettings(t, server, string(body))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := decodeSettings(t, rec).CavemanDefaultMode; got != "" {
				t.Errorf("caveman_default_mode = %q, want empty", got)
			}
			if got := config.Get().CavemanDefaultMode(); got != "" {
				t.Errorf("in-memory default = %q, want empty", got)
			}
		})
	}
}

func TestPutSettingsRejectsUnknownCavemanMode(t *testing.T) {
	server, _ := withCavemanSettings(t, "full")

	rec := putSettings(t, server, `{"caveman_default_mode":"shouty"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	// A rejected value must not disable or change the current default.
	if got := config.Get().CavemanDefaultMode(); got != "full" {
		t.Errorf("default changed to %q on a rejected update, want %q", got, "full")
	}
}

// A settings save that carries no caveman field must leave the level alone:
// the Web UI PUTs the whole settings object for unrelated changes too.
func TestPutSettingsWithoutCavemanFieldKeepsDefault(t *testing.T) {
	server, _ := withCavemanSettings(t, "ultra")

	rec := putSettings(t, server, `{"debug":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := config.Get().CavemanDefaultMode(); got != "ultra" {
		t.Errorf("default = %q, want %q", got, "ultra")
	}
}
