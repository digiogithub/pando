package models

import (
	"net/url"
	"os"
	"strings"
)

const (
	ProviderOllama       ModelProvider = "ollama"
	DefaultOllamaBaseURL               = "http://localhost:11434/v1"
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
