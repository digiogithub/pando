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
	"github.com/digiogithub/pando/internal/llm/models"
)

func resetProviderAccountsTestConfig(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	config.SetForTests(&config.Config{
		WorkingDir:       tmpDir,
		ProviderAccounts: nil,
		Providers:        map[models.ModelProvider]config.Provider{},
		MCPServers:       map[string]config.MCPServer{},
		LSP:              map[string]config.LSPConfig{},
	})
}

func TestProviderTypesOAuthMetadata(t *testing.T) {
	byType := map[string]ProviderTypeInfo{}
	for _, provider := range providerTypes {
		byType[provider.Type] = provider
	}
	if _, ok := byType["antigravity"]; ok {
		t.Fatal("antigravity provider type must not be offered")
	}
	if byType["anthropic"].SupportsOAuth {
		t.Fatal("anthropic is API-key only and must not advertise OAuth")
	}
	if !byType["anthropic"].RequiresAPIKey {
		t.Fatal("anthropic must require an API key")
	}
	copilot, ok := byType["copilot"]
	if !ok {
		t.Fatal("expected copilot provider type metadata to be present")
	}
	if copilot.RequiresAPIKey {
		t.Fatal("copilot must not require an API key")
	}
	if !copilot.SupportsOAuth {
		t.Fatal("copilot must advertise OAuth (GitHub login)")
	}
}

func TestProviderAccountToResponseMasksAPIKey(t *testing.T) {
	response := providerAccountToResponse(config.ProviderAccount{
		ID:     "openai-work",
		Type:   "openai",
		APIKey: "secret-api-key",
	}, true)

	if response.APIKey != "***-key" {
		t.Fatalf("api key = %q, want %q", response.APIKey, "***-key")
	}
}

func TestHandleGetProviderAccountMasksAPIKey(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:          "openai-work",
		DisplayName: "OpenAI Work",
		Type:        "openai",
		APIKey:      "sk-secret-value",
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/provider-accounts/openai-work", nil)
	req.SetPathValue("id", "openai-work")
	rec := httptest.NewRecorder()

	var server Server
	server.handleGetProviderAccount(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "sk-secret-value") {
		t.Fatalf("response leaked the API key: %s", rec.Body.String())
	}

	var got config.ProviderAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.APIKey != "***alue" {
		t.Fatalf("api key = %q, want %q", got.APIKey, "***alue")
	}
}

func TestHandleCreateProviderAccountDropsCopilotAPIKey(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	body := strings.NewReader(`{"id":"copilot","displayName":"GitHub Copilot","type":"copilot","apiKey":"should-be-dropped"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts", body)
	rec := httptest.NewRecorder()

	var server Server
	server.handleCreateProviderAccount(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	created, ok := config.GetProviderAccount("copilot")
	if !ok {
		t.Fatal("copilot account was not created")
	}
	if created.APIKey != "" {
		t.Fatalf("copilot account stored an API key %q, want none", created.APIKey)
	}
}

func TestHandleUpdateProviderAccountClearsCopilotAPIKey(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, config.ProviderAccount{
		ID:          "copilot",
		DisplayName: "GitHub Copilot",
		Type:        "copilot",
		APIKey:      "legacy-manual-token",
	})

	body := strings.NewReader(`{"displayName":"GitHub Copilot","type":"copilot","apiKey":"another-token"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/provider-accounts/copilot", body)
	req.SetPathValue("id", "copilot")
	rec := httptest.NewRecorder()

	var server Server
	server.handleUpdateProviderAccount(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	updated, ok := config.GetProviderAccount("copilot")
	if !ok {
		t.Fatal("copilot account missing after update")
	}
	if updated.APIKey != "" {
		t.Fatalf("copilot account kept an API key %q, want none", updated.APIKey)
	}
}
