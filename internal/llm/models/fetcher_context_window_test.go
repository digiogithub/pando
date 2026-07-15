package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGeminiModelsIncludesInputTokenLimitAsContextWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v1beta/models")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro","description":"desc","inputTokenLimit":1048576}]}`))
	}))
	defer server.Close()

	oldURL := geminiModelsURL
	geminiModelsURL = server.URL + "/v1beta/models"
	t.Cleanup(func() { geminiModelsURL = oldURL })

	models, err := fetchGeminiModels(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("fetchGeminiModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if models[0].ContextWindow != 1_048_576 {
		t.Fatalf("ContextWindow = %d, want %d", models[0].ContextWindow, 1_048_576)
	}
}

func TestFetchOpenRouterModelsIncludesContextLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/v1/models")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5","name":"GPT-5","description":"desc","created":123,"context_length":400000}]}`))
	}))
	defer server.Close()

	oldURL := openRouterModelsURL
	openRouterModelsURL = server.URL + "/api/v1/models"
	t.Cleanup(func() { openRouterModelsURL = oldURL })

	models, err := fetchOpenRouterModels(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("fetchOpenRouterModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if models[0].ContextWindow != 400_000 {
		t.Fatalf("ContextWindow = %d, want %d", models[0].ContextWindow, 400_000)
	}
}

func TestFetchCopilotModelsIncludesCapabilitiesLimitsAsContextWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4","name":"GPT-5.4","version":"gpt-5.4-2026-01-01","model_picker_enabled":true,"capabilities":{"limits":{"max_context_window_tokens":400000,"max_output_tokens":128000,"max_prompt_tokens":272000}}}]}`))
	}))
	defer server.Close()

	oldURL := copilotModelsURL
	copilotModelsURL = server.URL
	t.Cleanup(func() { copilotModelsURL = oldURL })

	models, err := fetchCopilotModels(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("fetchCopilotModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if models[0].ContextWindow != 400_000 {
		t.Fatalf("ContextWindow = %d, want %d", models[0].ContextWindow, 400_000)
	}
	if models[0].MaxOutputTokens != 128_000 {
		t.Fatalf("MaxOutputTokens = %d, want %d", models[0].MaxOutputTokens, 128_000)
	}
}

func TestFetchCopilotModelsFallsBackToMaxPromptTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4-mini","name":"GPT-5.4 mini","version":"gpt-5.4-mini-2026-01-01","model_picker_enabled":true,"capabilities":{"limits":{"max_output_tokens":64000,"max_prompt_tokens":200000}}}]}`))
	}))
	defer server.Close()

	oldURL := copilotModelsURL
	copilotModelsURL = server.URL
	t.Cleanup(func() { copilotModelsURL = oldURL })

	models, err := fetchCopilotModels(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("fetchCopilotModels() error = %v", err)
	}
	if models[0].ContextWindow != 200_000 {
		t.Fatalf("ContextWindow = %d, want %d", models[0].ContextWindow, 200_000)
	}
}

func TestFetchOpenRouterModelsPrefersTopProviderContextLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-5","name":"GPT-5","created":123,"context_length":128000,"top_provider":{"context_length":272000}}]}`))
	}))
	defer server.Close()

	oldURL := openRouterModelsURL
	openRouterModelsURL = server.URL
	t.Cleanup(func() { openRouterModelsURL = oldURL })

	models, err := fetchOpenRouterModels(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("fetchOpenRouterModels() error = %v", err)
	}
	if models[0].ContextWindow != 272_000 {
		t.Fatalf("ContextWindow = %d, want %d", models[0].ContextWindow, 272_000)
	}
}
