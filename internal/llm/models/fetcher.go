package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/version"
)

const defaultCopilotModelsURL = "https://api.githubcopilot.com/models"

var (
	geminiModelsURL     = "https://generativelanguage.googleapis.com/v1beta/models"
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	copilotModelsURL    = defaultCopilotModelsURL
)

// FetchedModel represents a model returned by a provider's API
type FetchedModel struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	Created         int64  `json:"created,omitempty"`
	ContextWindow   int64  `json:"context_window,omitempty"`
	MaxOutputTokens int64  `json:"max_output_tokens,omitempty"`
	// SupportedEndpoints lists the API routes the provider accepts for this
	// model (e.g. "/responses", "/chat/completions"). Empty when the provider's
	// listing API does not report it, in which case callers fall back to
	// name-based heuristics.
	SupportedEndpoints      []string `json:"supported_endpoints,omitempty"`
	CanReason               bool     `json:"can_reason,omitempty"`
	SupportsReasoningEffort bool     `json:"supports_reasoning_effort,omitempty"`
	SupportsAttachments     bool     `json:"supports_attachments,omitempty"`
}

// ProviderSupportsModelListing reports whether a provider exposes a model-listing
// API that FetchModelsFromProvider can query. Providers that return false (e.g.
// Azure, Vertex AI, Bedrock) rely on a curated static catalog instead, and must
// register per-account model copies separately to support multiple accounts.
func ProviderSupportsModelListing(provider ModelProvider) bool {
	switch provider {
	case ProviderCopilot, ProviderOpenAI, ProviderOllama, ProviderAnthropic,
		ProviderGemini, ProviderGROQ, ProviderOpenRouter, ProviderXAI,
		ProviderLlamaCpp, ProviderOpenAICompatible:
		return true
	default:
		return false
	}
}

// FetchModelsFromProvider queries a provider's API for available models
func FetchModelsFromProvider(ctx context.Context, provider ModelProvider, apiKey string, bearerToken string, baseURL string) ([]FetchedModel, error) {
	switch provider {
	case ProviderCopilot:
		return fetchCopilotModels(ctx, bearerToken)
	case ProviderOpenAI:
		return fetchOpenAIModels(ctx, apiKey)
	case ProviderOllama:
		return fetchOllamaModels(ctx, apiKey, baseURL)
	case ProviderAnthropic:
		return fetchAnthropicModels(ctx, apiKey, bearerToken)
	case ProviderGemini:
		return fetchGeminiModels(ctx, apiKey)
	case ProviderGROQ:
		return fetchGroqModels(ctx, apiKey)
	case ProviderOpenRouter:
		return fetchOpenRouterModels(ctx, apiKey)
	case ProviderXAI:
		return fetchXAIModels(ctx, apiKey)
	case ProviderLlamaCpp:
		return fetchLlamaCppModels(ctx, apiKey, baseURL)
	case ProviderOpenAICompatible:
		return fetchOpenAICompatibleModels(ctx, apiKey, baseURL)
	default:
		return nil, fmt.Errorf("provider %s does not support model listing", provider)
	}
}

func fetchAnthropicModels(ctx context.Context, apiKey, bearerToken string) ([]FetchedModel, error) {
	if apiKey == "" && bearerToken == "" {
		return nil, fmt.Errorf("API key or bearer token required for Anthropic")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	} else {
		req.Header.Set("x-api-key", apiKey)
	}

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			name := m.DisplayName
			if name == "" {
				name = m.ID
			}
			result = append(result, FetchedModel{
				ID:   m.ID,
				Name: name,
			})
		}
		return result, nil
	})
}

func fetchXAIModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for xAI")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.x.ai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			result = append(result, FetchedModel{
				ID:      m.ID,
				Name:    m.ID,
				Created: m.Created,
			})
		}
		return result, nil
	})
}

func fetchOllamaModels(ctx context.Context, apiKey string, baseURL string) ([]FetchedModel, error) {
	// Try OpenAI-compat endpoint first (/v1/models)
	modelsURL, err := url.JoinPath(ResolveOllamaBaseURL(baseURL), "models")
	if err != nil {
		return nil, fmt.Errorf("build Ollama models URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	result, err := doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}

		fetched := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			fetched = append(fetched, FetchedModel{
				ID:      m.ID,
				Name:    m.ID,
				Created: m.Created,
			})
		}

		return fetched, nil
	})

	if err == nil && len(result) > 0 {
		return result, nil
	}

	// Fallback to native Ollama API (/api/tags)
	return fetchOllamaModelsNative(ctx, apiKey, baseURL)
}

func fetchOllamaModelsNative(ctx context.Context, apiKey string, baseURL string) ([]FetchedModel, error) {
	tagsURL, err := url.JoinPath(ResolveOllamaRawBaseURL(baseURL), "api", "tags")
	if err != nil {
		return nil, fmt.Errorf("build Ollama tags URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Models []struct {
				Name  string `json:"name"`
				Model string `json:"model"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse native response: %w", err)
		}

		result := make([]FetchedModel, 0, len(response.Models))
		for _, m := range response.Models {
			id := m.Model
			if id == "" {
				id = m.Name
			}
			result = append(result, FetchedModel{
				ID:   id,
				Name: m.Name,
			})
		}

		return result, nil
	})
}

func fetchCopilotModels(ctx context.Context, bearerToken string) ([]FetchedModel, error) {
	if bearerToken == "" {
		return nil, fmt.Errorf("bearer token required for copilot")
	}

	// A caller (or a test) pointing copilotModelsURL somewhere else wants that
	// exact endpoint, so skip the token exchange and its host resolution.
	if copilotModelsURL != defaultCopilotModelsURL {
		return requestCopilotModels(ctx, copilotModelsURL, bearerToken)
	}

	// Exchanging the GitHub OAuth token for a Copilot API token is what makes
	// the seat's organization BYOK custom models show up in the listing; on
	// failure this returns the original token and the default host.
	apiToken, baseURL := auth.ResolveCopilotAPIAccess(ctx, bearerToken, "", "")
	modelsURL := copilotModelsURL
	if baseURL != "" {
		modelsURL = strings.TrimSuffix(baseURL, "/") + "/models"
	}

	fetched, err := requestCopilotModels(ctx, modelsURL, apiToken)
	if err == nil {
		return fetched, nil
	}
	// Fall back to the legacy call (raw OAuth token against the default host)
	// so a rejected or misrouted exchanged token never leaves the user without
	// a model list.
	if apiToken == bearerToken && modelsURL == copilotModelsURL {
		return nil, err
	}
	fallback, fallbackErr := requestCopilotModels(ctx, copilotModelsURL, bearerToken)
	if fallbackErr != nil {
		return nil, err
	}
	return fallback, nil
}

func requestCopilotModels(ctx context.Context, modelsURL, bearerToken string) ([]FetchedModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Pando/"+version.Version)
	req.Header.Set("Editor-Version", "Pando/"+version.Version)
	req.Header.Set("Editor-Plugin-Version", "Pando/"+version.Version)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID                 string   `json:"id"`
				Name               string   `json:"name"`
				Version            string   `json:"version"`
				ModelPickerEnabled bool     `json:"model_picker_enabled"`
				SupportedEndpoints []string `json:"supported_endpoints,omitempty"`
				Policy             *struct {
					State string `json:"state,omitempty"`
				} `json:"policy,omitempty"`
				Capabilities struct {
					Type   string `json:"type,omitempty"`
					Limits struct {
						MaxContextWindowTokens int64 `json:"max_context_window_tokens,omitempty"`
						MaxOutputTokens        int64 `json:"max_output_tokens,omitempty"`
						MaxPromptTokens        int64 `json:"max_prompt_tokens,omitempty"`
					} `json:"limits"`
					Supports struct {
						ReasoningEffort []string `json:"reasoning_effort,omitempty"`
						Vision          bool     `json:"vision,omitempty"`
						ToolCalls       bool     `json:"tool_calls,omitempty"`
					} `json:"supports"`
				} `json:"capabilities"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			// Filter out models not shown in the model picker or explicitly disabled.
			// Matches the opencode reference implementation filter logic.
			if !m.ModelPickerEnabled {
				continue
			}
			if m.Policy != nil && m.Policy.State == "disabled" {
				continue
			}
			// Non-chat models (embeddings, ...) cannot back a chat agent.
			if m.Capabilities.Type != "" && m.Capabilities.Type != "chat" {
				continue
			}
			// Prefer the explicit context window; fall back to max prompt tokens,
			// matching opencode's github-copilot plugin (models.ts).
			contextWindow := m.Capabilities.Limits.MaxContextWindowTokens
			if contextWindow == 0 {
				contextWindow = m.Capabilities.Limits.MaxPromptTokens
			}
			// The API is authoritative about reasoning and image support; relying
			// on model-name heuristics breaks every time a new family lands.
			supportsReasoningEffort := len(m.Capabilities.Supports.ReasoningEffort) > 0
			result = append(result, FetchedModel{
				ID:                      m.ID,
				Name:                    m.Name,
				ContextWindow:           contextWindow,
				MaxOutputTokens:         m.Capabilities.Limits.MaxOutputTokens,
				SupportedEndpoints:      m.SupportedEndpoints,
				CanReason:               supportsReasoningEffort,
				SupportsReasoningEffort: supportsReasoningEffort,
				SupportsAttachments:     m.Capabilities.Supports.Vision,
			})
		}
		return result, nil
	})
}

func fetchOpenAIModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for OpenAI")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			result = append(result, FetchedModel{
				ID:      m.ID,
				Name:    m.ID,
				Created: m.Created,
			})
		}
		return result, nil
	})
}

func fetchGeminiModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for Gemini")
	}

	url := fmt.Sprintf("%s?key=%s", geminiModelsURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Models []struct {
				Name            string `json:"name"`
				DisplayName     string `json:"displayName"`
				Description     string `json:"description"`
				InputTokenLimit int64  `json:"inputTokenLimit"`
			} `json:"models"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Models))
		for _, m := range response.Models {
			// Gemini returns names like "models/gemini-2.5-pro", strip the prefix
			id := strings.TrimPrefix(m.Name, "models/")
			result = append(result, FetchedModel{
				ID:            id,
				Name:          m.DisplayName,
				Description:   m.Description,
				ContextWindow: m.InputTokenLimit,
			})
		}
		return result, nil
	})
}

func fetchGroqModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for Groq")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.groq.com/openai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID            string `json:"id"`
				Created       int64  `json:"created"`
				ContextWindow int64  `json:"context_window"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			result = append(result, FetchedModel{
				ID:            m.ID,
				Name:          m.ID,
				Created:       m.Created,
				ContextWindow: m.ContextWindow,
			})
		}
		return result, nil
	})
}

func fetchOpenRouterModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for OpenRouter")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				Description   string `json:"description"`
				Created       int64  `json:"created"`
				ContextLength int64  `json:"context_length"`
				TopProvider   *struct {
					ContextLength       int64 `json:"context_length"`
					MaxCompletionTokens int64 `json:"max_completion_tokens,omitempty"`
				} `json:"top_provider,omitempty"`
				Architecture *struct {
					InputModalities []string `json:"input_modalities,omitempty"`
				} `json:"architecture,omitempty"`
				SupportedParameters []string `json:"supported_parameters,omitempty"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			contextWindow := m.ContextLength
			var maxOutputTokens int64
			if m.TopProvider != nil {
				if m.TopProvider.ContextLength > 0 {
					contextWindow = m.TopProvider.ContextLength
				}
				maxOutputTokens = m.TopProvider.MaxCompletionTokens
			}
			// OpenRouter reports capabilities per model: "reasoning"/"include_reasoning"
			// in supported_parameters, "image" among the input modalities.
			canReason := slices.Contains(m.SupportedParameters, "reasoning") ||
				slices.Contains(m.SupportedParameters, "include_reasoning")
			supportsAttachments := m.Architecture != nil &&
				slices.Contains(m.Architecture.InputModalities, "image")
			result = append(result, FetchedModel{
				ID:                      m.ID,
				Name:                    m.Name,
				Description:             m.Description,
				Created:                 m.Created,
				ContextWindow:           contextWindow,
				MaxOutputTokens:         maxOutputTokens,
				CanReason:               canReason,
				SupportsReasoningEffort: slices.Contains(m.SupportedParameters, "reasoning"),
				SupportsAttachments:     supportsAttachments,
			})
		}
		return result, nil
	})
}

func fetchOpenAICompatibleModels(ctx context.Context, apiKey, baseURL string) ([]FetchedModel, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("openai-compatible provider requires a base URL")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var result struct {
			Data []struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}

		fetched := make([]FetchedModel, 0, len(result.Data))
		for _, m := range result.Data {
			fetched = append(fetched, FetchedModel{ID: m.ID, Created: m.Created})
		}
		return fetched, nil
	})
}

// fetchLlamaCppModels queries a running llama-server for its loaded model(s)
// via the OpenAI-compatible /v1/models endpoint.  No API key is required.
func fetchLlamaCppModels(ctx context.Context, apiKey string, baseURL string) ([]FetchedModel, error) {
	modelsURL, err := url.JoinPath(ResolveLlamaCppBaseURL(baseURL), "models")
	if err != nil {
		return nil, fmt.Errorf("build llama-cpp models URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID      string `json:"id"`
				Created int64  `json:"created"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			result = append(result, FetchedModel{
				ID:      m.ID,
				Name:    m.ID,
				Created: m.Created,
			})
		}
		return result, nil
	})
}

type modelParser func(body []byte) ([]FetchedModel, error)

func doModelRequest(req *http.Request, parse modelParser) ([]FetchedModel, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parse(body)
}
