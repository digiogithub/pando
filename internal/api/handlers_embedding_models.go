package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// embeddingModelInfo describes one model offered by an embedding provider.
type embeddingModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Size is a human readable model size when the provider reports it
	// (Ollama returns the on-disk size of the pulled model).
	Size string `json:"size,omitempty"`
}

// embeddingModelsResponse is the payload of GET /api/v1/remembrances/embedding-models.
//
// Source tells the UI how trustworthy the list is:
//   - "api"       the provider was queried and it flagged these models as embedders
//   - "heuristic" the provider listed models but does not say which ones embed,
//     so the names were filtered
//   - "static"    the provider has no listing endpoint; a known catalog is returned
type embeddingModelsResponse struct {
	Provider string               `json:"provider"`
	Models   []embeddingModelInfo `json:"models"`
	Source   string               `json:"source"`
	Error    string               `json:"error,omitempty"`
}

// knownEmbeddingModels lists the embedding models of providers that do not
// expose a listing endpoint (or do not flag embedders in it). It is only used
// as a fallback: whenever the provider can be queried, the live answer wins.
var knownEmbeddingModels = map[string][]embeddingModelInfo{
	"openai": {
		{ID: "text-embedding-3-small", Name: "text-embedding-3-small"},
		{ID: "text-embedding-3-large", Name: "text-embedding-3-large"},
		{ID: "text-embedding-ada-002", Name: "text-embedding-ada-002"},
	},
	"anthropic": {
		{ID: "voyage-3", Name: "voyage-3"},
		{ID: "voyage-3-lite", Name: "voyage-3-lite"},
		{ID: "voyage-code-3", Name: "voyage-code-3"},
		{ID: "voyage-law-2", Name: "voyage-law-2"},
	},
}

// embeddingNameMarkers identify an embedding model by name, for providers that
// list their models without saying what each one does.
var embeddingNameMarkers = []string{
	"embed", "bge", "gte-", "e5-", "minilm", "nomic", "mxbai", "arctic-embed", "qwen3-embedding",
}

func looksLikeEmbeddingModel(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range embeddingNameMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// handleListEmbeddingModels returns the embedding models the configured provider
// offers, so the Remembrances settings can present a picker instead of asking
// the user to type a model name from memory.
//
// GET /api/v1/remembrances/embedding-models?provider=ollama&base_url=…
//
// provider and base_url are optional: they let the UI query the values being
// edited in the form before they are saved. Anything omitted falls back to the
// stored configuration.
func (s *Server) handleListEmbeddingModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(cfg.Remembrances.DocumentEmbeddingProvider))
	}
	if provider == "" {
		writeJSON(w, http.StatusOK, embeddingModelsResponse{
			Models: []embeddingModelInfo{},
			Source: "static",
			Error:  "no embedding provider selected",
		})
		return
	}

	baseURL := strings.TrimSpace(r.URL.Query().Get("base_url"))
	apiKey := strings.TrimSpace(r.URL.Query().Get("api_key"))
	if provider != "openai-compatible" {
		resolvedKey, resolvedBase := resolveProviderCredentials(cfg, provider)
		if apiKey == "" {
			apiKey = resolvedKey
		}
		if baseURL == "" {
			baseURL = resolvedBase
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	resp := listEmbeddingModels(ctx, provider, baseURL, apiKey)
	writeJSON(w, http.StatusOK, resp)
}

func listEmbeddingModels(ctx context.Context, provider, baseURL, apiKey string) embeddingModelsResponse {
	resp := embeddingModelsResponse{Provider: provider, Models: []embeddingModelInfo{}}

	switch provider {
	case "ollama":
		found, source, err := listOllamaEmbeddingModels(ctx, models.ResolveOllamaRawBaseURL(baseURL))
		resp.Models, resp.Source = found, source
		if err != nil {
			resp.Error = err.Error()
		}
	case "openai", "openai-compatible":
		found, err := listOpenAICompatibleEmbeddingModels(ctx, provider, baseURL, apiKey)
		if err != nil || len(found) == 0 {
			resp.Models = knownEmbeddingModels["openai"]
			resp.Source = "static"
			if err != nil {
				resp.Error = err.Error()
			}
			break
		}
		resp.Models, resp.Source = found, "heuristic"
	default:
		if known, ok := knownEmbeddingModels[provider]; ok {
			resp.Models, resp.Source = known, "static"
			break
		}
		resp.Source = "static"
		resp.Error = "provider does not publish an embedding model list"
	}

	if resp.Models == nil {
		resp.Models = []embeddingModelInfo{}
	}
	return resp
}

// ollamaTagsResponse is the subset of GET /api/tags this handler needs.
type ollamaTagsResponse struct {
	Models []struct {
		Model string `json:"model"`
		Name  string `json:"name"`
		Size  int64  `json:"size"`
	} `json:"models"`
}

// listOllamaEmbeddingModels asks Ollama which of the pulled models are embedders.
//
// Ollama does distinguish them, but only through POST /api/show, which reports a
// "capabilities" array containing "embedding" for embedding models (the model
// list in /api/tags says nothing about it). Older daemons do not return
// capabilities at all; in that case the model names are filtered instead, which
// is reported back as source "heuristic".
func listOllamaEmbeddingModels(ctx context.Context, rawBaseURL string) ([]embeddingModelInfo, string, error) {
	rawBaseURL = strings.TrimRight(rawBaseURL, "/")
	if rawBaseURL == "" {
		rawBaseURL = models.DefaultOllamaRawURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawBaseURL+"/api/tags", nil)
	if err != nil {
		return nil, "static", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	httpResp, err := client.Do(req)
	if err != nil {
		return nil, "static", fmt.Errorf("cannot reach Ollama at %s: %w", rawBaseURL, err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, "static", fmt.Errorf("ollama returned %s", httpResp.Status)
	}

	var tags ollamaTagsResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&tags); err != nil {
		return nil, "static", err
	}

	type candidate struct {
		info      embeddingModelInfo
		embedding bool
		known     bool
	}
	candidates := make([]candidate, len(tags.Models))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, m := range tags.Models {
		name := m.Model
		if name == "" {
			name = m.Name
		}
		candidates[i] = candidate{info: embeddingModelInfo{ID: name, Name: name, Size: humanBytes(m.Size)}}

		wg.Add(1)
		go func(idx int, model string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			caps, ok := ollamaModelCapabilities(ctx, client, rawBaseURL, model)
			if !ok {
				return
			}
			candidates[idx].known = true
			for _, c := range caps {
				if strings.EqualFold(c, "embedding") {
					candidates[idx].embedding = true
				}
			}
		}(i, name)
	}
	wg.Wait()

	result := make([]embeddingModelInfo, 0, len(candidates))
	source := "api"
	anyKnown := false
	for _, c := range candidates {
		if c.known {
			anyKnown = true
		}
	}
	for _, c := range candidates {
		match := c.embedding
		if !anyKnown {
			match = looksLikeEmbeddingModel(c.info.ID)
		} else if !c.known {
			match = looksLikeEmbeddingModel(c.info.ID)
		}
		if match {
			result = append(result, c.info)
		}
	}
	if !anyKnown {
		source = "heuristic"
	}

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, source, nil
}

// ollamaModelCapabilities returns the capability list Ollama reports for a model.
// The second value is false when the daemon does not report capabilities.
func ollamaModelCapabilities(ctx context.Context, client *http.Client, rawBaseURL, model string) ([]string, bool) {
	body, err := json.Marshal(map[string]string{"model": model})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawBaseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var show struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil {
		return nil, false
	}
	if len(show.Capabilities) == 0 {
		return nil, false
	}
	return show.Capabilities, true
}

// listOpenAICompatibleEmbeddingModels queries GET {base}/models and keeps the
// entries whose ID looks like an embedding model: neither OpenAI nor the
// OpenAI-compatible servers report per-model capabilities in that endpoint.
func listOpenAICompatibleEmbeddingModels(ctx context.Context, provider, baseURL, apiKey string) ([]embeddingModelInfo, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		if provider == "openai" {
			base = "https://api.openai.com/v1"
		} else {
			return nil, fmt.Errorf("base URL is required for %s", provider)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", provider, resp.Status)
	}

	var listing struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, err
	}

	result := make([]embeddingModelInfo, 0, len(listing.Data))
	for _, m := range listing.Data {
		if looksLikeEmbeddingModel(m.ID) {
			result = append(result, embeddingModelInfo{ID: m.ID, Name: m.ID})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// humanBytes renders a byte count the way a model listing should show it.
func humanBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
