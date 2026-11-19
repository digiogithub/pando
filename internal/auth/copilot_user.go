package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/version"
)

const (
	// CopilotAPIVersion is sent as X-GitHub-Api-Version on every request to the
	// Copilot API host (models, chat, responses, auto routing). It pins the
	// response shapes Pando parses, e.g. billing.token_prices.cache_read_price.
	CopilotAPIVersion = "2026-08-01"
	// copilotUserAPIVersion is the version of the api.github.com account
	// endpoint, which is versioned separately from the Copilot API host.
	copilotUserAPIVersion = "2025-04-01"
	copilotUserPath       = "/copilot_internal/user"

	copilotEndpointCacheTTL        = 30 * time.Minute
	copilotEndpointFailureCooldown = 10 * time.Minute
)

// copilotUserURLOverride replaces the account endpoint URL in tests.
var copilotUserURLOverride string

// CopilotClientHeaders returns the headers that identify Pando to the Copilot
// API. The per-seat hosts reject requests that do not identify an editor.
func CopilotClientHeaders() map[string]string {
	return map[string]string{
		"User-Agent":             "Pando/" + version.Version,
		"Editor-Version":         "Pando/" + version.Version,
		"Editor-Plugin-Version":  "Pando/" + version.Version,
		"Copilot-Integration-Id": "vscode-chat",
		"X-GitHub-Api-Version":   CopilotAPIVersion,
	}
}

// ApplyCopilotClientHeaders sets CopilotClientHeaders on an outgoing request.
func ApplyCopilotClientHeaders(header http.Header) {
	for key, value := range CopilotClientHeaders() {
		header.Set(key, value)
	}
}

// CopilotUser is the subset of GET /copilot_internal/user Pando relies on.
type CopilotUser struct {
	// ChatEnabled is nil when GitHub did not report it.
	ChatEnabled         *bool
	CanSignupForLimited bool
	Plan                string
	// APIEndpoint is the Copilot API host of the account's plan, for example
	// https://api.business.githubcopilot.com. It changes with the plan.
	APIEndpoint string
}

func copilotUserURL(enterpriseURL string) string {
	if copilotUserURLOverride != "" {
		return copilotUserURLOverride
	}
	domain := resolveGitHubDomain(enterpriseURL)
	if domain == copilotDefaultGitHubDomain {
		return "https://api.github.com" + copilotUserPath
	}
	return fmt.Sprintf("https://api.%s%s", domain, copilotUserPath)
}

// FetchCopilotUser reads the Copilot account record of a GitHub OAuth token.
// OAuth itself succeeds for any GitHub account, so this is the only signal that
// tells whether the account can chat and which API host serves it.
func FetchCopilotUser(ctx context.Context, token, enterpriseURL string) (*CopilotUser, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("github oauth token is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotUserURL(enterpriseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("create copilot user request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Pando/"+version.Version)
	req.Header.Set("X-GitHub-Api-Version", copilotUserAPIVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch copilot user: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read copilot user response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch copilot user failed: status %d", resp.StatusCode)
	}
	return parseCopilotUser(body)
}

func parseCopilotUser(body []byte) (*CopilotUser, error) {
	var payload struct {
		ChatEnabled         *bool  `json:"chat_enabled"`
		CanSignupForLimited bool   `json:"can_signup_for_limited"`
		Plan                string `json:"copilot_plan"`
		Endpoints           struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse copilot user response: %w", err)
	}
	return &CopilotUser{
		ChatEnabled:         payload.ChatEnabled,
		CanSignupForLimited: payload.CanSignupForLimited,
		Plan:                payload.Plan,
		APIEndpoint:         strings.TrimRight(strings.TrimSpace(payload.Endpoints.API), "/"),
	}, nil
}

// CopilotEntitlementError returns a user-facing message when the account
// explicitly has no Copilot chat access, and "" otherwise. Only an explicit
// chat_enabled=false counts: a missing field must not turn into a denial.
func CopilotEntitlementError(user *CopilotUser) string {
	if user == nil || user.ChatEnabled == nil || *user.ChatEnabled {
		return ""
	}
	if user.CanSignupForLimited {
		return "This GitHub account is not signed up for GitHub Copilot. Sign up for Copilot Free at https://github.com/features/copilot/plans and log in again."
	}
	return "This GitHub account does not have GitHub Copilot access. It needs an active Copilot subscription or a seat assigned by an organization."
}

type copilotEndpointEntry struct {
	endpoint string
	at       time.Time
}

var (
	copilotEndpointCacheMu sync.Mutex
	copilotEndpointCache   = map[string]copilotEndpointEntry{}
)

// DiscoverCopilotAPIEndpoint returns the Copilot API host GitHub advertises for
// the account behind a raw OAuth token, or "" when it cannot be determined.
// Successful lookups are cached for a while, and failures for a shorter time so
// a broken token does not add a round trip to every request.
func DiscoverCopilotAPIEndpoint(ctx context.Context, token, enterpriseURL string) string {
	token = strings.TrimSpace(token)
	if token == "" || IsCopilotAPIToken(token) {
		return ""
	}
	cacheKey := enterpriseURL + "\x00" + token

	copilotEndpointCacheMu.Lock()
	if entry, ok := copilotEndpointCache[cacheKey]; ok {
		ttl := copilotEndpointCacheTTL
		if entry.endpoint == "" {
			ttl = copilotEndpointFailureCooldown
		}
		if time.Since(entry.at) < ttl {
			copilotEndpointCacheMu.Unlock()
			return entry.endpoint
		}
	}
	copilotEndpointCacheMu.Unlock()

	endpoint := ""
	if user, err := FetchCopilotUser(ctx, token, enterpriseURL); err == nil && user != nil {
		endpoint = user.APIEndpoint
	}

	copilotEndpointCacheMu.Lock()
	copilotEndpointCache[cacheKey] = copilotEndpointEntry{endpoint: endpoint, at: time.Now()}
	copilotEndpointCacheMu.Unlock()
	return endpoint
}
