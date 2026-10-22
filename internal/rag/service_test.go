package rag

import "testing"

func TestResolveEmbedderCredentialsOllamaHonorsCustomBaseURL(t *testing.T) {
	_, baseURL := resolveEmbedderCredentials("ollama", "", " http://localhost:11435/v1/ ")
	if baseURL != "http://localhost:11435" {
		t.Fatalf("baseURL = %q, want http://localhost:11435", baseURL)
	}
}

func TestResolveEmbedderCredentialsOpenAICompatibleUsesCustomValues(t *testing.T) {
	apiKey, baseURL := resolveEmbedderCredentials("openai-compatible", "sk-test", "http://embed.local/v1")
	if apiKey != "sk-test" || baseURL != "http://embed.local/v1" {
		t.Fatalf("got (%q, %q)", apiKey, baseURL)
	}
}
