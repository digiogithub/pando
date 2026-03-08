package models

import (
	"net/url"
	"os"
	"strings"
)

const (
	ProviderOllama       ModelProvider = "ollama"
	DefaultOllamaBaseURL               = "http://localhost:11434/v1"
	DefaultOllamaRawURL                = "http://localhost:11434"
)

func ResolveOllamaBaseURL(configured string) string {
	baseURL := strings.TrimSpace(configured)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = DefaultOllamaBaseURL
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}

	switch strings.TrimRight(parsed.Path, "/") {
	case "", "/":
		parsed.Path = "/v1"
	case "/api":
		parsed.Path = "/v1"
	case "/v1":
		parsed.Path = "/v1"
	}

	return strings.TrimRight(parsed.String(), "/")
}

// ResolveOllamaRawBaseURL returns the raw Ollama base URL without the /v1 suffix,
// suitable for calling native Ollama endpoints like /api/tags.
func ResolveOllamaRawBaseURL(configured string) string {
	baseURL := strings.TrimSpace(configured)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL"))
	}
	if baseURL == "" {
		return DefaultOllamaRawURL
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}

	switch strings.TrimRight(parsed.Path, "/") {
	case "", "/", "/api", "/v1":
		parsed.Path = ""
	}

	return strings.TrimRight(parsed.String(), "/")
}
