package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModelsFromProviderOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/v1/models")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"llama3.2","created":123},{"id":"qwen2.5-coder","created":456}]}`))
	}))
	defer server.Close()

	models, err := FetchModelsFromProvider(context.Background(), ProviderOllama, "", "", server.URL)
	if err != nil {
		t.Fatalf("FetchModelsFromProvider() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "llama3.2" {
		t.Fatalf("models[0].ID = %q, want %q", models[0].ID, "llama3.2")
	}
}

func TestResolveOllamaBaseURL(t *testing.T) {
	tests := map[string]string{
		"":                           DefaultOllamaBaseURL,
		"http://localhost:11434":     "http://localhost:11434/v1",
		"http://localhost:11434/api": "http://localhost:11434/v1",
		"http://localhost:11434/v1":  "http://localhost:11434/v1",
		"https://ollama.com":         "https://ollama.com/v1",
		"https://ollama.com/custom":  "https://ollama.com/custom",
	}

	for input, want := range tests {
		if got := ResolveOllamaBaseURL(input); got != want {
			t.Fatalf("ResolveOllamaBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveOllamaRawBaseURL(t *testing.T) {
	tests := map[string]string{
		"":                           DefaultOllamaRawURL,
		"http://localhost:11434":     "http://localhost:11434",
		"http://localhost:11434/api": "http://localhost:11434",
		"http://localhost:11434/v1":  "http://localhost:11434",
		"https://ollama.com":         "https://ollama.com",
		"https://ollama.com/custom":  "https://ollama.com/custom",
	}

	for input, want := range tests {
		if got := ResolveOllamaRawBaseURL(input); got != want {
			t.Fatalf("ResolveOllamaRawBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFetchOllamaModelsNativeFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			// Simulate old Ollama: returns 404
			http.NotFound(w, r)
		case "/api/tags":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3:latest","model":"llama3:latest"},{"name":"qwen2.5-coder:7b","model":"qwen2.5-coder:7b"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	models, err := FetchModelsFromProvider(context.Background(), ProviderOllama, "", "", server.URL)
	if err != nil {
		t.Fatalf("FetchModelsFromProvider() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "llama3:latest" {
		t.Fatalf("models[0].ID = %q, want %q", models[0].ID, "llama3:latest")
	}
	if models[1].Name != "qwen2.5-coder:7b" {
		t.Fatalf("models[1].Name = %q, want %q", models[1].Name, "qwen2.5-coder:7b")
	}
}
