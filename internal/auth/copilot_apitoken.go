package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/version"
)

// The Copilot API accepts two kinds of bearer tokens:
//
//  1. the raw GitHub OAuth token (gho_/ghu_...), which resolves to the generic
//     Copilot catalog, and
//  2. the short-lived Copilot API token obtained by exchanging that OAuth token
//     at /copilot_internal/v2/token, which carries the seat context
//     (organization/enterprise) of the user.
//
// Only the exchanged token exposes organization BYOK "custom models" (ids like
// "<org>/<provider>/<model>") and the per-seat API host (for example
// https://api.business.githubcopilot.com). Editors do this exchange; Pando used
// to send the raw OAuth token, so those models were never listed.
//
// The exchange is best-effort: whenever it fails the caller keeps using the raw
// OAuth token and the default host, which is the historical behaviour.

const (
	copilotAPITokenPath      = "/copilot_internal/v2/token"
	copilotTokenRenewMargin  = 2 * time.Minute
	copilotExchangeUserAgent = "GithubCopilot/1.155.0"
)

// CopilotAPIToken is the result of exchanging a GitHub OAuth token for a
// short-lived Copilot API token.
type CopilotAPIToken struct {
	Token         string
	ExpiresAt     int64
	APIEndpoint   string
	Organizations []string
	SKU           string
}

func (t *CopilotAPIToken) expired() bool {
	if t == nil || strings.TrimSpace(t.Token) == "" {
		return true
	}
	if t.ExpiresAt <= 0 {
		// No expiry reported: treat as single-use and re-exchange next time.
		return true
	}
	return time.Now().Add(copilotTokenRenewMargin).Unix() >= t.ExpiresAt
}

var (
	copilotTokenCacheMu sync.Mutex
	copilotTokenCache   = map[string]*CopilotAPIToken{}
	// Tokens issued by apps without Copilot entitlement fail forever; remember
	// the failure for a while so every request does not retry the exchange.
	copilotTokenFailures = map[string]time.Time{}
)

const copilotExchangeFailureCooldown = 10 * time.Minute

// CopilotTokenExchangeDisabled reports whether the user opted out of the token
// exchange, forcing the legacy behaviour of sending the raw OAuth token.
func CopilotTokenExchangeDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PANDO_COPILOT_TOKEN_EXCHANGE"))) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// IsCopilotAPIToken reports whether the token already is an exchanged Copilot
// API token (they are opaque "tid=...;exp=..." strings) rather than a GitHub
// OAuth token. Exchanging one of these again is not possible.
func IsCopilotAPIToken(token string) bool {
	return strings.Contains(token, "tid=")
}

// ExchangeCopilotAPIToken trades a GitHub OAuth token for a short-lived Copilot
// API token. Results are cached in memory until shortly before expiry.
func ExchangeCopilotAPIToken(ctx context.Context, oauthToken, enterpriseURL string) (*CopilotAPIToken, error) {
	oauthToken = strings.TrimSpace(oauthToken)
	if oauthToken == "" {
		return nil, fmt.Errorf("github oauth token is empty")
	}
	if IsCopilotAPIToken(oauthToken) {
		return nil, fmt.Errorf("token is already a copilot api token")
	}
	if CopilotTokenExchangeDisabled() {
		return nil, fmt.Errorf("copilot token exchange disabled by PANDO_COPILOT_TOKEN_EXCHANGE")
	}

	cacheKey := enterpriseURL + "\x00" + oauthToken

	copilotTokenCacheMu.Lock()
	if cached, ok := copilotTokenCache[cacheKey]; ok && !cached.expired() {
		copilotTokenCacheMu.Unlock()
		return cached, nil
	}
	if failedAt, ok := copilotTokenFailures[cacheKey]; ok && time.Since(failedAt) < copilotExchangeFailureCooldown {
		copilotTokenCacheMu.Unlock()
		return nil, fmt.Errorf("copilot token exchange recently failed for this token")
	}
	copilotTokenCacheMu.Unlock()

	token, err := fetchCopilotAPIToken(ctx, oauthToken, enterpriseURL)
	if err != nil {
		copilotTokenCacheMu.Lock()
		copilotTokenFailures[cacheKey] = time.Now()
		copilotTokenCacheMu.Unlock()
		return nil, err
	}

	copilotTokenCacheMu.Lock()
	delete(copilotTokenFailures, cacheKey)
	copilotTokenCache[cacheKey] = token
	copilotTokenCacheMu.Unlock()
	return token, nil
}

func fetchCopilotAPIToken(ctx context.Context, oauthToken, enterpriseURL string) (*CopilotAPIToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenExchangeURL(enterpriseURL), nil)
	if err != nil {
		return nil, fmt.Errorf("create copilot token exchange request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+oauthToken)
	// GitHub only serves the seat token to clients identifying as an editor.
	req.Header.Set("User-Agent", copilotExchangeUserAgent)
	req.Header.Set("Editor-Version", "Pando/"+version.Version)
	req.Header.Set("Editor-Plugin-Version", "Pando/"+version.Version)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange copilot token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read copilot token exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot token exchange failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseCopilotAPIToken(body)
}

func parseCopilotAPIToken(body []byte) (*CopilotAPIToken, error) {
	var payload struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		SKU       string `json:"sku"`
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
		OrganizationList []string `json:"organization_list"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse copilot token exchange response: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return nil, fmt.Errorf("copilot token exchange response has no token")
	}
	return &CopilotAPIToken{
		Token:         payload.Token,
		ExpiresAt:     payload.ExpiresAt,
		APIEndpoint:   strings.TrimSuffix(strings.TrimSpace(payload.Endpoints.API), "/"),
		Organizations: payload.OrganizationList,
		SKU:           payload.SKU,
	}, nil
}

func copilotTokenExchangeURL(enterpriseURL string) string {
	domain := resolveGitHubDomain(enterpriseURL)
	if domain == copilotDefaultGitHubDomain {
		return "https://api.github.com" + copilotAPITokenPath
	}
	// GitHub Enterprise Server exposes the REST API under /api/v3.
	return fmt.Sprintf("https://%s/api/v3%s", domain, copilotAPITokenPath)
}

// exchangeAnyLocalToken walks the GitHub OAuth tokens available on this machine
// and returns the first Copilot API token one of them yields, skipping the token
// already tried. It returns nil when none of them can be exchanged.
func exchangeAnyLocalToken(ctx context.Context, alreadyTried, enterpriseURL string) *CopilotAPIToken {
	if CopilotTokenExchangeDisabled() {
		return nil
	}
	for _, candidate := range GitHubOAuthTokenCandidates() {
		if candidate.Token == alreadyTried || IsCopilotAPIToken(candidate.Token) {
			continue
		}
		if exchanged, err := ExchangeCopilotAPIToken(ctx, candidate.Token, enterpriseURL); err == nil && exchanged != nil {
			return exchanged
		}
	}
	return nil
}

// ResolveCopilotAPIAccess returns the bearer token and API base URL to use for
// Copilot requests. It exchanges the GitHub OAuth token for a Copilot API token
// so that organization BYOK custom models become visible, and falls back to the
// supplied token and base URL when the exchange is unavailable.
//
// configuredBaseURL, when non-empty, always wins: an explicitly configured host
// must not be silently replaced by the one advertised by GitHub.
func ResolveCopilotAPIAccess(ctx context.Context, token, enterpriseURL, configuredBaseURL string) (string, string) {
	fallbackBaseURL := strings.TrimSpace(configuredBaseURL)
	if fallbackBaseURL == "" {
		fallbackBaseURL = CopilotAPIBaseURL(enterpriseURL)
	}
	fallbackBaseURL = strings.TrimSuffix(fallbackBaseURL, "/")

	if strings.TrimSpace(token) == "" || IsCopilotAPIToken(token) {
		return token, fallbackBaseURL
	}

	exchanged, err := ExchangeCopilotAPIToken(ctx, token, enterpriseURL)
	if err != nil || exchanged == nil || strings.TrimSpace(exchanged.Token) == "" {
		// The supplied token may be issued by an app without Copilot entitlement
		// (Pando's own device-flow token is one such case). Any other token
		// found locally that does exchange yields the same seat, so try those
		// before giving up on the custom models.
		exchanged = exchangeAnyLocalToken(ctx, token, enterpriseURL)
	}
	if exchanged == nil || strings.TrimSpace(exchanged.Token) == "" {
		return token, fallbackBaseURL
	}

	baseURL := fallbackBaseURL
	if strings.TrimSpace(configuredBaseURL) == "" && exchanged.APIEndpoint != "" {
		baseURL = exchanged.APIEndpoint
	}
	return exchanged.Token, baseURL
}
