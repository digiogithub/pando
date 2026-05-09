package llmproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

const (
	googleSearchAPIURL = "https://www.googleapis.com/customsearch/v1"
	exaSearchAPIURL    = "https://api.exa.ai/search"
	braveSearchAPIURL  = "https://api.search.brave.com/res/v1/web/search"
)

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearcher can perform web searches.
type WebSearcher interface {
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
}

// googleSearcher performs searches using Google Custom Search API.
type googleSearcher struct {
	client       *http.Client
	apiKey       string
	searchEngineID string
}

func (g *googleSearcher) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults < 1 || maxResults > 10 {
		maxResults = 10
	}

	q := url.Values{}
	q.Set("key", g.apiKey)
	q.Set("cx", g.searchEngineID)
	q.Set("q", query)
	q.Set("num", fmt.Sprint(maxResults))

	req, err := http.NewRequestWithContext(ctx, "GET", googleSearchAPIURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("google search: failed to create request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google search: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("google search: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google search: API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("google search: failed to parse response: %w", err)
	}

	results := make([]SearchResult, 0, len(result.Items))
	for _, item := range result.Items {
		results = append(results, SearchResult{
			Title:   item.Title,
			URL:     item.Link,
			Snippet: item.Snippet,
		})
	}
	return results, nil
}

// exaSearcher performs searches using the Exa AI Search API.
type exaSearcher struct {
	client *http.Client
	apiKey string
}

func (e *exaSearcher) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults < 1 || maxResults > 100 {
		maxResults = 10
	}

	reqBody := map[string]any{
		"query":      query,
		"numResults": maxResults,
		"type":       "auto",
		"contents": map[string]any{
			"highlights": map[string]any{
				"maxCharacters": 400,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("exa search: failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", exaSearchAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("exa search: failed to create request: %w", err)
	}
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exa search: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("exa search: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exa search: API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Results []struct {
			Title      string   `json:"title"`
			URL        string   `json:"url"`
			Highlights []string `json:"highlights"`
			Text       string   `json:"text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("exa search: failed to parse response: %w", err)
	}

	results := make([]SearchResult, 0, len(result.Results))
	for _, item := range result.Results {
		snippet := ""
		if len(item.Highlights) > 0 {
			snippet = item.Highlights[0]
		} else if item.Text != "" {
			if len(item.Text) > 400 {
				snippet = item.Text[:400] + "..."
			} else {
				snippet = item.Text
			}
		}
		results = append(results, SearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: snippet,
		})
	}
	return results, nil
}

// braveSearcher performs searches using the Brave Search API.
type braveSearcher struct {
	client *http.Client
	apiKey string
}

func (b *braveSearcher) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if maxResults < 1 || maxResults > 20 {
		maxResults = 10
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("count", fmt.Sprint(maxResults))

	req, err := http.NewRequestWithContext(ctx, "GET", braveSearchAPIURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("brave search: failed to create request: %w", err)
	}
	req.Header.Set("X-Subscription-Token", b.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("brave search: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search: API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("brave search: failed to parse response: %w", err)
	}

	results := make([]SearchResult, 0, len(result.Web.Results))
	for _, item := range result.Web.Results {
		results = append(results, SearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Description,
		})
	}
	return results, nil
}

// NewWebSearchChain returns a slice of configured WebSearcher implementations
// in priority order: Google, Exa, Brave. Providers without valid configuration
// are omitted from the chain.
func NewWebSearchChain() []WebSearcher {
	cfg := config.Get()
	if cfg == nil {
		return nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var chain []WebSearcher

	// Google
	if cfg.InternalTools.GoogleSearchEnabled &&
		strings.TrimSpace(cfg.InternalTools.GoogleAPIKey) != "" &&
		strings.TrimSpace(cfg.InternalTools.GoogleSearchEngineID) != "" {
		chain = append(chain, &googleSearcher{
			client:         client,
			apiKey:         cfg.InternalTools.GoogleAPIKey,
			searchEngineID: cfg.InternalTools.GoogleSearchEngineID,
		})
	}

	// Exa
	if cfg.InternalTools.ExaSearchEnabled &&
		strings.TrimSpace(cfg.InternalTools.ExaAPIKey) != "" {
		chain = append(chain, &exaSearcher{
			client: client,
			apiKey: cfg.InternalTools.ExaAPIKey,
		})
	}

	// Brave
	if cfg.InternalTools.BraveSearchEnabled &&
		strings.TrimSpace(cfg.InternalTools.BraveAPIKey) != "" {
		chain = append(chain, &braveSearcher{
			client: client,
			apiKey: cfg.InternalTools.BraveAPIKey,
		})
	}

	return chain
}

// SearchWithFallback attempts each provider in the chain in order, returning
// the first non-empty result set. If all providers fail or the chain is empty,
// an error is returned.
func SearchWithFallback(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	chain := NewWebSearchChain()
	if len(chain) == 0 {
		return nil, fmt.Errorf("no web search providers configured")
	}

	var lastErr error
	for _, searcher := range chain {
		results, err := searcher.Search(ctx, query, maxResults)
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) > 0 {
			return results, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("all search providers returned empty results for: %s", query)
}

// FormatSearchResultsForPrompt formats a slice of SearchResult values as a
// numbered, human-readable list suitable for injection into a prompt message.
func FormatSearchResultsForPrompt(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Web search results:\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		if strings.TrimSpace(r.Snippet) != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
