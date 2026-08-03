package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// registerCachedModel simulates a model restored from an on-disk cache written by
// an older build: present in SupportedModels() and dynamicModels, but missing the
// endpoint metadata the current code routes on.
func registerCachedModel(t *testing.T, m Model) {
	t.Helper()
	RegisterDynamicModel(m)
	t.Cleanup(func() {
		dynamicModels.Delete(m.ID)
		DeleteSupportedModels(m.ID)
	})
}

func TestRefreshProviderModelsUpdatesStaleCachedMetadata(t *testing.T) {
	registerCachedModel(t, Model{
		ID:       "copilot.mai-code-1-flash-picker",
		Name:     "Copilot: MAI-Code-1-Flash",
		Provider: ProviderCopilot,
		APIModel: "mai-code-1-flash-picker",
		// No SupportedEndpoints: this is the stale cache entry.
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"mai-code-1-flash-picker","name":"MAI-Code-1-Flash","model_picker_enabled":true,
			 "supported_endpoints":["/responses"],
			 "capabilities":{"type":"chat","limits":{"max_context_window_tokens":256000}}}
		]}`))
	}))
	defer server.Close()

	oldURL := copilotModelsURL
	copilotModelsURL = server.URL
	t.Cleanup(func() { copilotModelsURL = oldURL })

	if err := RefreshProviderModels(context.Background(), ProviderCopilot, "", "token", ""); err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", err)
	}

	got := SupportedModels()["copilot.mai-code-1-flash-picker"]
	if len(got.SupportedEndpoints) != 1 || got.SupportedEndpoints[0] != CopilotEndpointResponses {
		t.Fatalf("SupportedEndpoints = %v, want [%s]", got.SupportedEndpoints, CopilotEndpointResponses)
	}
	if !CopilotModelUsesResponsesAPI(got) {
		t.Fatal("CopilotModelUsesResponsesAPI() = false, want true after refresh")
	}
}

func TestRefreshProviderModelsForAccountUpdatesStaleCachedMetadata(t *testing.T) {
	registerCachedModel(t, Model{
		ID:        "copilot.mai-code-1-flash-picker",
		Name:      "Copilot: MAI-Code-1-Flash",
		Provider:  ProviderCopilot,
		APIModel:  "mai-code-1-flash-picker",
		AccountID: "acct",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"mai-code-1-flash-picker","name":"MAI-Code-1-Flash","model_picker_enabled":true,
			 "supported_endpoints":["/responses"],
			 "capabilities":{"type":"chat","limits":{"max_context_window_tokens":256000}}}
		]}`))
	}))
	defer server.Close()

	oldURL := copilotModelsURL
	copilotModelsURL = server.URL
	t.Cleanup(func() { copilotModelsURL = oldURL })

	err := RefreshProviderModelsForAccount(context.Background(), AccountModelRefreshParams{
		AccountID:         "acct",
		ProviderType:      ProviderCopilot,
		BearerToken:       "token",
		AllAccountsOfType: 1,
	})
	if err != nil {
		t.Fatalf("RefreshProviderModelsForAccount() error = %v", err)
	}

	got := SupportedModels()["copilot.mai-code-1-flash-picker"]
	if !CopilotModelUsesResponsesAPI(got) {
		t.Fatalf("CopilotModelUsesResponsesAPI() = false (endpoints %v), want true after refresh", got.SupportedEndpoints)
	}
}

func TestLoadModelCacheDropsUnversionedCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacy := map[ModelID]Model{
		"copilot.mai-code-1-flash-picker": {
			ID:       "copilot.mai-code-1-flash-picker",
			Provider: ProviderCopilot,
			APIModel: "mai-code-1-flash-picker",
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, cacheFileName), data, 0600); err != nil {
		t.Fatalf("write legacy cache: %v", err)
	}

	if err := LoadModelCache(); err != nil {
		t.Fatalf("LoadModelCache() error = %v", err)
	}
	if _, ok := SupportedModels()["copilot.mai-code-1-flash-picker"]; ok {
		t.Fatal("unversioned cache entry was loaded; want it dropped")
	}
}

func TestModelCacheRoundTripPreservesEndpoints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	registerCachedModel(t, Model{
		ID:                 "copilot.roundtrip-model",
		Provider:           ProviderCopilot,
		APIModel:           "roundtrip-model",
		SupportedEndpoints: []string{CopilotEndpointResponses},
	})

	if err := SaveModelCache(); err != nil {
		t.Fatalf("SaveModelCache() error = %v", err)
	}

	DeleteSupportedModels("copilot.roundtrip-model")
	dynamicModels.Delete(ModelID("copilot.roundtrip-model"))

	if err := LoadModelCache(); err != nil {
		t.Fatalf("LoadModelCache() error = %v", err)
	}
	got := SupportedModels()["copilot.roundtrip-model"]
	if len(got.SupportedEndpoints) != 1 || got.SupportedEndpoints[0] != CopilotEndpointResponses {
		t.Fatalf("SupportedEndpoints = %v, want [%s]", got.SupportedEndpoints, CopilotEndpointResponses)
	}
}
