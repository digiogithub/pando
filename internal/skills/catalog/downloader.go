package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

var githubHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
}

// FetchSkillContent attempts to fetch SKILL.md content from a GitHub repository.
// source must be in "owner/repo" format. skillName is the skill identifier.
// It tries multiple URL patterns and returns the first successful response.
func FetchSkillContent(ctx context.Context, source, skillName string) (string, error) {
	patterns := []string{
		"https://raw.githubusercontent.com/%s/main/skills/%s/SKILL.md",
		"https://raw.githubusercontent.com/%s/main/%s/SKILL.md",
		"https://raw.githubusercontent.com/%s/main/SKILL.md",
		"https://raw.githubusercontent.com/%s/main/.agents/skills/%s/SKILL.md",
	}

	var lastErr error
	for _, pattern := range patterns {
		var rawURL string
		// Pattern 3 (index 2) has no skillName placeholder
		if pattern == "https://raw.githubusercontent.com/%s/main/SKILL.md" {
			rawURL = fmt.Sprintf(pattern, source)
		} else {
			rawURL = fmt.Sprintf(pattern, source, skillName)
		}

		content, err := fetchURL(ctx, rawURL)
		if err == nil {
			return content, nil
		}
		lastErr = err
	}

	return "", fmt.Errorf("catalog: skill %q not found in %q: %w", skillName, source, lastErr)
}

func fetchURL(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}
