package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

	// Normalize the model ID and try to find its provider.
	normalizedID := models.NormalizeModelID(req.Model)

	// Find a matching provider account.
	accounts := config.GetProviderAccounts()

	// Try to find from SupportedModels first.
	var providerType models.ModelProvider
	if m, ok := models.SupportedModels[normalizedID]; ok {
		providerType = m.Provider
	}

	// Find the account for this provider.
	var account *config.ProviderAccount
	for i := range accounts {
		if accounts[i].Disabled {
			continue
		}
		if providerType != "" && accounts[i].Type == providerType {
			a := accounts[i]
			account = &a
			break
		}
	}

	// If we couldn't find via SupportedModels, try matching model prefix against account types.
	// Dynamic models use the format "{provider}.{model}" e.g. "openai.text-embedding-3-small".
	if account == nil {
		for i := range accounts {
			if accounts[i].Disabled {
				continue
			}
			prefix := string(accounts[i].Type) + "."
			if len(string(normalizedID)) > len(prefix) && string(normalizedID)[:len(prefix)] == prefix {
				a := accounts[i]
				account = &a
				providerType = a.Type
				break
			}
		}
	}

	// If still not found, use first non-disabled account as fallback (for unknown model IDs).
	if account == nil && len(accounts) > 0 {
		for i := range accounts {
			if !accounts[i].Disabled {
				a := accounts[i]
				account = &a
				providerType = a.Type
				break
			}
		}
	}

	if account == nil {
		writeError(w, http.StatusUnprocessableEntity, "no configured provider account found for model")
		return
	}

	// Determine the base URL for the embeddings call based on provider type.
	baseURL, apiKey, err := resolveEmbeddingsEndpoint(account, providerType)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Build the upstream URL.
	embURL := baseURL + "/embeddings"

	// Forward the request.
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, embURL, bytes.NewReader(rawBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to build upstream request: %s", err))
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
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
