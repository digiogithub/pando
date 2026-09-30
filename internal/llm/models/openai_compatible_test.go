package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/llm/models/modelsdev"
)

// TestFetchOpenAICompatibleModelsReadsOpenRouterShape verifies that gateways
// answering with an OpenRouter-shaped listing (Kilo, …) keep their context
// window and output cap instead of falling back to the 128K guess.
func TestFetchOpenAICompatibleModelsReadsOpenRouterShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/gateway/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"deepseek/deepseek-v4.1-flash","name":"DeepSeek: V4.1 Flash","context_length":1048576,
			 "top_provider":{"context_length":1048576,"max_completion_tokens":943718},
			 "architecture":{"input_modalities":["text","image"]},
			 "supported_parameters":["reasoning","tools"]},
			{"id":"plain-model","object":"model","created":42}
		]}`))
	}))
	defer srv.Close()

	got, err := fetchOpenAICompatibleModels(context.Background(), "key", srv.URL+"/api/gateway")
	if err != nil {
		t.Fatalf("fetchOpenAICompatibleModels() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d models, want 2", len(got))
	}
	rich := got[0]
	if rich.ContextWindow != 1048576 || rich.MaxOutputTokens != 943718 {
		t.Errorf("rich model limits = %d/%d, want 1048576/943718", rich.ContextWindow, rich.MaxOutputTokens)
	}
	if rich.Name != "DeepSeek: V4.1 Flash" || !rich.CanReason || !rich.SupportsAttachments {
		t.Errorf("rich model metadata not decoded: %+v", rich)
	}
	plain := got[1]
	if plain.ID != "plain-model" || plain.Created != 42 || plain.ContextWindow != 0 {
		t.Errorf("plain model decoded as %+v", plain)
	}
}

// TestOpenAICompatibleAccountEnrichedByBaseURL verifies that an
// openai-compatible account whose base URL matches a models.dev provider API
// (OpenCode Zen here, whose listing reports only ids) gets the catalog's
// context window instead of the 128K fallback.
func TestOpenAICompatibleAccountEnrichedByBaseURL(t *testing.T) {
	seedModelsDevCatalog(t, map[string]any{
		"opencode": map[string]any{
			"id":  "opencode",
			"api": "https://opencode.ai/zen/v1",
			"models": map[string]any{
				"minimax-m3": map[string]any{
					"id":    "minimax-m3",
					"limit": map[string]any{"context": 400000, "output": 64000},
					"cost":  map[string]any{"input": 0.3, "output": 1.2},
				},
			},
		},
	})

	params := AccountModelRefreshParams{
		AccountID:         "zen",
		ProviderType:      ProviderOpenAICompatible,
		BaseURL:           "https://opencode.ai/zen/v1/",
		AllAccountsOfType: 1,
	}
	RememberAccountBaseURL(params.AccountID, params.BaseURL)
	t.Cleanup(func() { RememberAccountBaseURL(params.AccountID, "") })

	got := modelFromFetchedAccountModel(context.Background(), params, FetchedModel{ID: "minimax-m3"})
	if got.ContextWindow != 400000 || got.DefaultMaxTokens != 64000 {
		t.Errorf("limits = %d/%d, want 400000/64000", got.ContextWindow, got.DefaultMaxTokens)
	}
	if got.CostPer1MIn != 0.3 {
		t.Errorf("CostPer1MIn = %v, want 0.3", got.CostPer1MIn)
	}

	// An account on an unknown host keeps the guessed default.
	other := params
	other.AccountID = "elsewhere"
	RememberAccountBaseURL(other.AccountID, "https://llm.internal.example/v1")
	t.Cleanup(func() { RememberAccountBaseURL(other.AccountID, "") })
	fallback := modelFromFetchedAccountModel(context.Background(), other, FetchedModel{ID: "minimax-m3"})
	if fallback.ContextWindow != 128_000 {
		t.Errorf("unknown host ContextWindow = %d, want 128000 fallback", fallback.ContextWindow)
	}
}

// seedModelsDevCatalog serves a fixed catalog through the on-disk cache in an
// isolated HOME, enabling the catalog only for the calling test.
func seedModelsDevCatalog(t *testing.T, providers map[string]any) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	payload, err := json.Marshal(providers)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := json.Marshal(map[string]any{
		"version":    1,
		"fetched_at": time.Now(),
		"payload":    json.RawMessage(payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".pando_modelsdev.json"), cache, 0o600); err != nil {
		t.Fatal(err)
	}
	modelsdev.Reset()
	modelsdev.SetDisabled(false)
	t.Cleanup(func() {
		modelsdev.SetDisabled(true)
		modelsdev.Reset()
	})
}
