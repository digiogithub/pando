package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://skills.sh"
	defaultTimeout = 10 * time.Second
	defaultLimit   = 10
)

// CatalogSkill represents a skill entry from the skills.sh catalog.
type CatalogSkill struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
	Source   string `json:"source"`
}

// SearchResult is the response from the skills.sh search API.
type SearchResult struct {
	Query      string         `json:"query"`
	SearchType string         `json:"searchType"`
	Skills     []CatalogSkill `json:"skills"`
	Count      int            `json:"count"`
	DurationMs int            `json:"duration_ms"`
}

// Client is an HTTP client for the skills.sh catalog API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new catalog client.
// If baseURL is empty, it defaults to "https://skills.sh".
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// Search queries the skills.sh catalog for skills matching the given query.
// If limit is <= 0, it defaults to 10.
func (c *Client) Search(ctx context.Context, query string, limit int) (*SearchResult, error) {
	if limit <= 0 {
		limit = defaultLimit
	}

	endpoint := c.baseURL + "/api/search"
	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", strconv.Itoa(limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("catalog: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("catalog: unmarshal response: %w", err)
	}

	return &result, nil
}

// GetContent fetches the raw SKILL.md content for a skill identified by skillID.
// It fetches from {baseURL}/api/skills/{skillID}/content.
func (c *Client) GetContent(ctx context.Context, skillID string) (string, error) {
	endpoint := c.baseURL + "/api/skills/" + url.PathEscape(skillID) + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("catalog: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("catalog: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("catalog: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("catalog: HTTP %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// FormatInstalls formats an install count into a human-readable string.
// Examples: 0 → "0 installs", 1500 → "1.5K installs", 3000000 → "3M installs".
func FormatInstalls(count int) string {
	switch {
	case count >= 1_000_000:
		val := float64(count) / 1_000_000
		return formatFloat(val) + "M installs"
	case count >= 1_000:
		val := float64(count) / 1_000
		return formatFloat(val) + "K installs"
	default:
		return strconv.Itoa(count) + " installs"
	}
}

// formatFloat formats a float removing trailing zeros after one decimal place.
func formatFloat(val float64) string {
	s := fmt.Sprintf("%.1f", val)
	// Remove trailing zero: "1.0" → "1", "1.5" → "1.5"
	if len(s) > 2 && s[len(s)-1] == '0' && s[len(s)-2] == '.' {
		return s[:len(s)-2]
	}
	return s
}
