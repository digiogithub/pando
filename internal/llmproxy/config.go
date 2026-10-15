package llmproxy

import "net/http"

// ProxyConfig holds configuration for the LLM proxy server.
type ProxyConfig struct {
	Host   string
	Port   int
	APIKey string // optional, for Bearer auth
	Debug  bool
	// Convert, when non-nil, is mounted at POST /v1/convert behind the same
	// API key (see convert.NewHTTPHandler). Nil leaves the endpoint off.
	Convert http.Handler
}
