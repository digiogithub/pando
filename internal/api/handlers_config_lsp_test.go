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

// withLSPConfig points the configuration at a throwaway working directory so
// the activation endpoints write to a temporary .pando.toml.
func withLSPConfig(t *testing.T) *Server {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, ".pando.toml")
	if err := os.WriteFile(file, []byte("Debug = true\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	prev := config.Get()
	config.SetForTests(&config.Config{
		WorkingDir:      dir,
		Debug:           true,
		LSPAutoActivate: true,
		LSPAutoInstall:  true,
		LSP: map[string]config.LSPConfig{
			"gopls": {Command: "gopls", Languages: []string{".go"}},
		},
	})
	t.Cleanup(func() { config.SetForTests(prev) })

	return &Server{}
}

func TestGetConfigLSPIncludesActivation(t *testing.T) {
	server := withLSPConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/lsp", nil)
	rec := httptest.NewRecorder()
	server.handleConfigLSP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		LSP        []LSPConfigItem              `json:"lsp"`
		Activation config.LSPActivationSettings `json:"activation"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.LSP) != 1 || resp.LSP[0].Language != "gopls" {
		t.Fatalf("unexpected servers: %+v", resp.LSP)
	}
	if resp.Activation.ActivateOn != config.LSPActivateEdits || !resp.Activation.AutoInstall {
		t.Fatalf("unexpected activation: %+v", resp.Activation)
	}
}

func TestPutConfigLSPActivation(t *testing.T) {
	server := withLSPConfig(t)

	body := `{"autoActivate":true,"activateOn":"reads","autoInstall":false,"startupTimeout":"30s","installTimeout":"3m"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/lsp/activation", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleConfigLSPActivation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := config.Get().LSPActivationSettings()
	if got.ActivateOn != config.LSPActivateReads || got.AutoInstall {
		t.Fatalf("settings not applied: %+v", got)
	}
	if got.StartupTimeout != "30s" || got.InstallTimeout != "3m0s" {
		t.Fatalf("timeouts = %q / %q", got.StartupTimeout, got.InstallTimeout)
	}
}

func TestPutConfigLSPActivationRejectsBadMode(t *testing.T) {
	server := withLSPConfig(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/lsp/activation", strings.NewReader(`{"activateOn":"sometimes"}`))
	rec := httptest.NewRecorder()
	server.handleConfigLSPActivation(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConfigLSPCatalogRequiresApp(t *testing.T) {
	server := withLSPConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/lsp/catalog", nil)
	rec := httptest.NewRecorder()
	server.handleConfigLSPCatalog(rec, req)

	// Without an application the endpoint degrades instead of panicking.
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}
