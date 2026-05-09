package llmproxy

// ProxyConfig holds configuration for the LLM proxy server.
type ProxyConfig struct {
	Host   string
	Port   int
	APIKey string // optional, for Bearer auth
	Debug  bool
}
