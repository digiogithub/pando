package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models/modelsdev"
)

// withModelsDevSettings points the config at a throwaway working directory so
// UpdateModelsDev writes there instead of the developer's real ~/.pando.toml.
func withModelsDevSettings(t *testing.T, enabled bool) *Server {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".pando.toml"), []byte("Debug = true\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	prev := config.Get()
	prevDisabled := modelsdev.IsDisabled()
	config.SetForTests(&config.Config{
		WorkingDir: dir,
		Debug:      true,
		ModelsDev:  config.ModelsDevConfig{Enabled: enabled},
	})
	t.Cleanup(func() {
		config.SetForTests(prev)
		modelsdev.SetDisabled(prevDisabled)
	})

	return &Server{}
}

func TestGetSettingsExposesModelsDevEnabled(t *testing.T) {
	server := withModelsDevSettings(t, true)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !decodeSettings(t, rec).ModelsDevEnabled {
		t.Error("models_dev_enabled should mirror the configured value")
	}
}

// Turning the toggle off must reach the catalog immediately, not only after a
// restart: the switch is read on every Get.
func TestPutSettingsTogglesModelsDevCatalog(t *testing.T) {
	server := withModelsDevSettings(t, true)
	modelsdev.SetDisabled(false)

	rec := putSettings(t, server, `{"models_dev_enabled": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if !modelsdev.IsDisabled() {
		t.Error("disabling the setting must disable the catalog in the running process")
	}
	if decodeSettings(t, rec).ModelsDevEnabled {
		t.Error("response should report the new value")
	}

	rec = putSettings(t, server, `{"models_dev_enabled": true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if modelsdev.IsDisabled() {
		t.Error("re-enabling the setting must re-enable the catalog")
	}
}
