package models

import (
	"net/url"
	"os"
	"strings"
)

const (
	ProviderLlamaCpp       ModelProvider = "llama-cpp"
	DefaultLlamaCppBaseURL               = "http://localhost:8080/v1"
)

// ResolveLlamaCppBaseURL returns the llama-server base URL, consulting the
// LLAMACPP_BASE_URL environment variable as a fallback before using the
// built-in default (http://localhost:8080/v1).
func ResolveLlamaCppBaseURL(configured string) string {
	baseURL := strings.TrimSpace(configured)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("LLAMACPP_BASE_URL"))
	}
	if baseURL == "" {
		return DefaultLlamaCppBaseURL
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}

	// Normalise to end with /v1 — llama-server serves the OpenAI-compat
	// API under that path prefix.
	switch strings.TrimRight(parsed.Path, "/") {
	case "", "/":
		parsed.Path = "/v1"
	case "/v1":
		// already correct
	default:
		// Keep whatever the user specified; they may have a reverse-proxy.
	}

	return strings.TrimRight(parsed.String(), "/")
}
