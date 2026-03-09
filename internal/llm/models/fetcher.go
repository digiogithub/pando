package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchedModel represents a model returned by a provider's API
type FetchedModel struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Created     int64  `json:"created,omitempty"`
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
		return fetchAnthropicModels(ctx, apiKey)
	case ProviderGemini:
		return fetchGeminiModels(ctx, apiKey)
	case ProviderGROQ:
		return fetchGroqModels(ctx, apiKey)
	case ProviderOpenRouter:
		return fetchOpenRouterModels(ctx, apiKey)
	case ProviderXAI:
		return fetchXAIModels(ctx, apiKey)
	default:
		return nil, fmt.Errorf("provider %s does not support model listing", provider)
	}
}

func fetchAnthropicModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for Anthropic")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.githubcopilot.com/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Accept", "application/json")

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		// Copilot returns a JSON array of model objects
		var response struct {
			Data []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			// Try as a plain array
			var models []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Version string `json:"version"`
			}
			if err2 := json.Unmarshal(body, &models); err2 != nil {
				return nil, fmt.Errorf("parse response: %w", err)
			}
			result := make([]FetchedModel, 0, len(models))
			for _, m := range models {
				result = append(result, FetchedModel{
					ID:   m.ID,
					Name: m.Name,
				})
			}
			return result, nil
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			result = append(result, FetchedModel{
				ID:   m.ID,
				Name: m.Name,
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

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Models []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Description string `json:"description"`
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
				ID:          id,
				Name:        m.DisplayName,
				Description: m.Description,
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

func fetchOpenRouterModels(ctx context.Context, apiKey string) ([]FetchedModel, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for OpenRouter")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	return doModelRequest(req, func(body []byte) ([]FetchedModel, error) {
		var response struct {
			Data []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Created     int64  `json:"created"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		result := make([]FetchedModel, 0, len(response.Data))
		for _, m := range response.Data {
			result = append(result, FetchedModel{
				ID:          m.ID,
				Name:        m.Name,
				Description: m.Description,
				Created:     m.Created,
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
