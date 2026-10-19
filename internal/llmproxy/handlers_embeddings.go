package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// openAIEmbeddingRequest represents an OpenAI-compatible embeddings request.
type openAIEmbeddingRequest struct {
	Model          string      `json:"model"`
	Input          interface{} `json:"input"` // string or []string
	EncodingFormat string      `json:"encoding_format,omitempty"`
	Dimensions     *int        `json:"dimensions,omitempty"`
}

// handleEmbeddings handles POST /v1/embeddings as a passthrough to the underlying provider.
func (s *LLMProxyServer) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Read request body so we can re-use it when forwarding.
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req openAIEmbeddingRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %s", err))
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	account, providerType, upstreamModel, ok := resolveEmbeddingsTarget(req.Model)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "no configured provider account found for model")
		return
	}

	// Determine the base URL for the embeddings call based on provider type.
	baseURL, apiKey, err := resolveEmbeddingsEndpoint(account, providerType)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// The upstream knows the model by its own name, never by the proxy's
	// "<account>." or "<type>." prefixed id.
	body, err := rewriteEmbeddingsModel(rawBody, upstreamModel)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %s", err))
		return
	}

	// Build the upstream URL.
	embURL := baseURL + "/embeddings"

	// Forward the request.
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, embURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to build upstream request: %s", err))
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	for k, v := range account.ExtraHeaders {
		upstreamReq.Header.Set(k, v)
	}
	if apiKey != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %s", err))
		return
	}
	defer resp.Body.Close()

	// Copy response headers and status code, then stream body.
	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// resolveEmbeddingsEndpoint returns the base URL and API key for the given provider account.
// Returns an error for providers that do not support embeddings.
func resolveEmbeddingsEndpoint(account *config.ProviderAccount, providerType models.ModelProvider) (baseURL, apiKey string, err error) {
	apiKey = account.APIKey

	switch providerType {
	case models.ProviderOpenAI:
		baseURL = "https://api.openai.com/v1"

	case models.ProviderOllama:
		baseURL = models.ResolveOllamaBaseURL(account.BaseURL)

	case models.ProviderLlamaCpp:
		baseURL = models.ResolveLlamaCppBaseURL(account.BaseURL)

	case models.ProviderOpenRouter:
		baseURL = "https://openrouter.ai/api/v1"

	case models.ProviderAzure:
		if account.BaseURL == "" {
			return "", "", fmt.Errorf("azure provider requires a configured BaseURL for embeddings")
		}
		baseURL = account.BaseURL

	case models.ProviderOpenAICompatible:
		baseURL = account.BaseURL

	case models.ProviderCopilot:
		// Load Copilot / GitHub OAuth token.
		if token, tokenErr := auth.LoadGitHubOAuthToken(); tokenErr == nil && token != "" {
			apiKey = token
		} else if session, sessErr := auth.LoadCopilotSession(); sessErr == nil && session != nil {
			apiKey = session.AccessToken
		}
		// Copilot API proxies to OpenAI under the hood.
		baseURL = "https://api.githubcopilot.com/v1"

	default:
		// Anthropic, Gemini, Bedrock, VertexAI, GROQ, etc. do not support the /v1/embeddings endpoint.
		return "", "", fmt.Errorf("provider %q does not support embeddings", providerType)
	}

	return baseURL, apiKey, nil
}

// resolveEmbeddingsTarget picks the provider account that serves the requested
// embeddings model and the model name the upstream expects (without any
// "<account>." or "<type>." prefix). It prefers the same resolution as chat,
// then falls back to the first active account that supports embeddings.
func resolveEmbeddingsTarget(requested string) (account *config.ProviderAccount, providerType models.ModelProvider, upstreamModel string, ok bool) {
	if rm, found := resolveModel(requested); found {
		acc := rm.account
		upstream := rm.model.APIModel
		if upstream == "" {
			upstream = requested
		}
		return &acc, acc.Type, upstream, true
	}
	for _, acc := range activeAccounts() {
		if !providerSupportsEmbeddings(acc.Type) {
			continue
		}
		a := acc
		bare, _ := stripAccountPrefix(strings.TrimSpace(requested), a)
		return &a, a.Type, bare, true
	}
	return nil, "", "", false
}

// providerSupportsEmbeddings reports whether the proxy can forward
// /v1/embeddings to the given provider type.
func providerSupportsEmbeddings(t models.ModelProvider) bool {
	switch t {
	case models.ProviderOpenAI, models.ProviderOllama, models.ProviderLlamaCpp,
		models.ProviderOpenRouter, models.ProviderAzure, models.ProviderOpenAICompatible,
		models.ProviderCopilot:
		return true
	}
	return false
}

// rewriteEmbeddingsModel returns the request body with its "model" field set to
// model, preserving every other field untouched.
func rewriteEmbeddingsModel(raw []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	m, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	fields["model"] = m
	return json.Marshal(fields)
}
