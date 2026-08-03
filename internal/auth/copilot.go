package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/version"
	"gopkg.in/yaml.v3"
)

const (
	CopilotClientID                = "Ov23li8tweQw6odWQebz"
	copilotDefaultGitHubDomain     = "github.com"
	copilotPollingSafetyMargin     = 3 * time.Second
	copilotSessionProvider         = "github-copilot"
	copilotSessionRelativeFilePath = "pando/auth/github-copilot.json"
)

type CopilotSession struct {
	Provider      string `json:"provider,omitempty"`
	AccessToken   string `json:"access_token"`
	TokenType     string `json:"token_type,omitempty"`
	Scope         string `json:"scope,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
	EnterpriseURL string `json:"enterprise_url,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
}

type CopilotDeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

type copilotDeviceTokenResponse struct {
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
	Interval         int    `json:"interval,omitempty"`
}

type CopilotAuthStatus struct {
	Authenticated bool
	Source        string
	EnterpriseURL string
	Message       string
}

func NormalizeGitHubDomain(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimSuffix(trimmed, "/")
	return trimmed
}

func CopilotSessionFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, copilotSessionRelativeFilePath), nil
}

func LoadCopilotSession() (*CopilotSession, error) {
	filePath, err := CopilotSessionFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var session CopilotSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse copilot session: %w", err)
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return nil, fmt.Errorf("copilot session is missing access token")
	}
	return &session, nil
}

func SaveCopilotSession(session CopilotSession) error {
	if strings.TrimSpace(session.AccessToken) == "" {
		return fmt.Errorf("copilot session access token cannot be empty")
	}
	if session.Provider == "" {
		session.Provider = copilotSessionProvider
	}
	if session.CreatedAt == 0 {
		session.CreatedAt = time.Now().Unix()
	}

	filePath, err := CopilotSessionFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create copilot auth directory: %w", err)
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal copilot session: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return fmt.Errorf("write copilot session: %w", err)
	}
	return nil
}

func DeleteCopilotSession() error {
	filePath, err := CopilotSessionFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete copilot session: %w", err)
	}
	return nil
}

// isCopilotCompatibleToken reports whether the token is an OAuth token accepted
// by the Copilot API.  The Copilot API rejects Personal Access Tokens (PATs),
// which start with "ghp_" or "github_pat_".  Only OAuth tokens ("gho_") and
// tokens without a recognised prefix (legacy / enterprise) are accepted.
func isCopilotCompatibleToken(token string) bool {
	lower := strings.ToLower(strings.TrimSpace(token))
	if strings.HasPrefix(lower, "ghp_") {
		return false // Personal Access Token — rejected by Copilot API
	}
	if strings.HasPrefix(lower, "github_pat_") {
		return false // Fine-grained PAT — also rejected
	}
	return token != ""
}

// GitHubTokenCandidate is a GitHub OAuth token discovered locally, tagged with
// where it came from.
type GitHubTokenCandidate struct {
	Source string
	Token  string
}

// GitHubOAuthTokenCandidates returns every usable GitHub OAuth token found on
// this machine, in the same priority order LoadGitHubOAuthToken applies. Not all
// of them can be exchanged for a Copilot API token (only tokens issued to an
// app with Copilot entitlement can), so callers that need the exchanged token
// walk the list instead of taking just the first entry.
func GitHubOAuthTokenCandidates() []GitHubTokenCandidate {
	var candidates []GitHubTokenCandidate
	add := func(source, token string) {
		token = strings.TrimSpace(token)
		if token == "" {
			return
		}
		for _, existing := range candidates {
			if existing.Token == token {
				return
			}
		}
		candidates = append(candidates, GitHubTokenCandidate{Source: source, Token: token})
	}

	// COPILOT_GITHUB_TOKEN is user-explicit and always wins regardless of type.
	add("COPILOT_GITHUB_TOKEN", os.Getenv("COPILOT_GITHUB_TOKEN"))

	// GH_TOKEN is the GitHub CLI token — typically an OAuth token, try it next.
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); isCopilotCompatibleToken(token) {
		add("GH_TOKEN", token)
	}

	// Saved session from `pando auth copilot login` takes precedence over the
	// generic GITHUB_TOKEN env var, which may be a PAT (ghp_...) used for CI/CD
	// and is rejected by the Copilot API.
	if session, err := LoadCopilotSession(); err == nil && session != nil {
		add("pando-session", session.AccessToken)
	}

	for _, token := range loadLegacyCopilotTokens() {
		add("github-copilot-app", token)
	}

	if token, err := loadGitHubCLIToken(); err == nil {
		add("gh-cli", token)
	}

	// GITHUB_TOKEN is checked last because it is commonly set to a PAT in CI
	// environments.  Only accept it when it looks like an OAuth token.
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); isCopilotCompatibleToken(token) {
		add("GITHUB_TOKEN", token)
	}

	return candidates
}

func LoadGitHubOAuthToken() (string, error) {
	candidates := GitHubOAuthTokenCandidates()
	if len(candidates) == 0 {
		return "", fmt.Errorf("GitHub Copilot token not found; run `pando auth copilot login`")
	}
	return candidates[0].Token, nil
}

func CopilotAPIBaseURL(enterpriseURL string) string {
	domain := resolveGitHubDomain(enterpriseURL)
	if domain == copilotDefaultGitHubDomain {
		return "https://api.githubcopilot.com"
	}
	return fmt.Sprintf("https://copilot-api.%s", domain)
}

func StartCopilotDeviceFlow(ctx context.Context, enterpriseURL string) (*CopilotDeviceCode, error) {
	deviceCodeURL, _ := oauthEndpoints(resolveGitHubDomain(enterpriseURL))
	payload := map[string]string{
		"client_id": CopilotClientID,
		"scope":     "read:user",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal device flow payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create device flow request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Pando/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("start device flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("start device flow failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var deviceCode CopilotDeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return nil, fmt.Errorf("decode device flow response: %w", err)
	}
	if deviceCode.Interval <= 0 {
		deviceCode.Interval = 5
	}
	return &deviceCode, nil
}

func PollCopilotDeviceFlow(ctx context.Context, enterpriseURL string, deviceCode *CopilotDeviceCode) (*CopilotSession, error) {
	if deviceCode == nil {
		return nil, fmt.Errorf("device code is required")
	}
	_, accessTokenURL := oauthEndpoints(resolveGitHubDomain(enterpriseURL))
	interval := deviceCode.Interval
	if interval <= 0 {
		interval = 5
	}

	for {
		payload := map[string]string{
			"client_id":   CopilotClientID,
			"device_code": deviceCode.DeviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal token poll payload: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create token poll request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Pando/"+version.Version)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll device flow token: %w", err)
		}

		var tokenResp copilotDeviceTokenResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&tokenResp)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode device flow token response: %w", decodeErr)
		}
		if resp.StatusCode != http.StatusOK {
			if tokenResp.Error != "" {
				return nil, fmt.Errorf("device flow token request failed: %s", tokenResp.Error)
			}
			return nil, fmt.Errorf("device flow token request failed: status %d", resp.StatusCode)
		}

		if strings.TrimSpace(tokenResp.AccessToken) != "" {
			session := &CopilotSession{
				Provider:      copilotSessionProvider,
				AccessToken:   tokenResp.AccessToken,
				TokenType:     tokenResp.TokenType,
				Scope:         tokenResp.Scope,
				EnterpriseURL: normalizedEnterpriseURL(enterpriseURL),
				CreatedAt:     time.Now().Unix(),
			}
			return session, nil
		}

		switch tokenResp.Error {
		case "authorization_pending":
			if err := sleepWithContext(ctx, time.Duration(interval)*time.Second+copilotPollingSafetyMargin); err != nil {
				return nil, err
			}
			continue
		case "slow_down":
			interval += 5
			if tokenResp.Interval > 0 {
				interval = tokenResp.Interval
			}
			if err := sleepWithContext(ctx, time.Duration(interval)*time.Second+copilotPollingSafetyMargin); err != nil {
				return nil, err
			}
			continue
		case "expired_token", "access_denied", "unsupported_grant_type", "incorrect_device_code", "bad_verification_code":
			if tokenResp.ErrorDescription != "" {
				return nil, fmt.Errorf("device flow failed: %s", tokenResp.ErrorDescription)
			}
			return nil, fmt.Errorf("device flow failed: %s", tokenResp.Error)
		default:
			if tokenResp.Error != "" {
				return nil, fmt.Errorf("device flow failed: %s", tokenResp.Error)
			}
		}

		if err := sleepWithContext(ctx, time.Duration(interval)*time.Second+copilotPollingSafetyMargin); err != nil {
			return nil, err
		}
	}
}

func CompleteCopilotDeviceFlow(ctx context.Context, enterpriseURL string, deviceCode *CopilotDeviceCode) (*CopilotSession, error) {
	session, err := PollCopilotDeviceFlow(ctx, enterpriseURL, deviceCode)
	if err != nil {
		return nil, err
	}
	if err := ValidateCopilotToken(ctx, *session); err != nil {
		return nil, err
	}
	if err := SaveCopilotSession(*session); err != nil {
		return nil, err
	}
	return session, nil
}

func CopilotDeviceFlowInstructions(deviceCode CopilotDeviceCode) string {
	return fmt.Sprintf("Open this URL in your browser: %s\nEnter this code: %s", deviceCode.VerificationURI, deviceCode.UserCode)
}

func GetCopilotAuthStatus() CopilotAuthStatus {
	session, err := LoadCopilotSession()
	if err == nil && session != nil {
		location := copilotDefaultGitHubDomain
		if strings.TrimSpace(session.EnterpriseURL) != "" {
			location = session.EnterpriseURL
		}
		return CopilotAuthStatus{
			Authenticated: true,
			Source:        "saved-session",
			EnterpriseURL: session.EnterpriseURL,
			Message:       fmt.Sprintf("GitHub Copilot authenticated via saved session (%s).", location),
		}
	}

	token, loadErr := LoadGitHubOAuthToken()
	if loadErr == nil && strings.TrimSpace(token) != "" {
		return CopilotAuthStatus{
			Authenticated: true,
			Source:        "external-token",
			Message:       "GitHub Copilot token available via environment or external GitHub tooling.",
		}
	}

	return CopilotAuthStatus{
		Authenticated: false,
		Source:        "none",
		Message:       "GitHub Copilot is not authenticated. Run `pando auth copilot login`.",
	}
}

func ValidateCopilotToken(ctx context.Context, session CopilotSession) error {
	token := strings.TrimSpace(session.AccessToken)
	if token == "" {
		return fmt.Errorf("copilot access token cannot be empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CopilotAPIBaseURL(session.EnterpriseURL)+"/models", nil)
	if err != nil {
		return fmt.Errorf("create validation request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Pando/"+version.Version)
	// The per-seat hosts (api.business/api.enterprise.githubcopilot.com) reject
	// requests that do not identify an editor with 400 model_not_supported.
	req.Header.Set("Editor-Version", "Pando/"+version.Version)
	req.Header.Set("Editor-Plugin-Version", "Pando/"+version.Version)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("Openai-Intent", "conversation-edits")
	req.Header.Set("x-initiator", "user")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("validate copilot token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("validate copilot token failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

// CheckCopilotModelsAPI verifies that the Copilot models API endpoint is
// reachable using the provided bearer token and base URL.
func CheckCopilotModelsAPI(token, baseURL string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("copilot token is empty")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = CopilotAPIBaseURL("")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("create models API request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Pando/"+version.Version)
	// The per-seat hosts (api.business/api.enterprise.githubcopilot.com) reject
	// requests that do not identify an editor with 400 model_not_supported.
	req.Header.Set("Editor-Version", "Pando/"+version.Version)
	req.Header.Set("Editor-Plugin-Version", "Pando/"+version.Version)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("Openai-Intent", "conversation-edits")
	req.Header.Set("x-initiator", "user")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("copilot models API unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("copilot models API error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func resolveGitHubDomain(enterpriseURL string) string {
	if domain := NormalizeGitHubDomain(enterpriseURL); domain != "" {
		return domain
	}
	if domain := NormalizeGitHubDomain(os.Getenv("GH_HOST")); domain != "" {
		return domain
	}
	return copilotDefaultGitHubDomain
}

func normalizedEnterpriseURL(enterpriseURL string) string {
	domain := NormalizeGitHubDomain(enterpriseURL)
	if domain == "" || domain == copilotDefaultGitHubDomain {
		return ""
	}
	return domain
}

func oauthEndpoints(domain string) (string, string) {
	return fmt.Sprintf("https://%s/login/device/code", domain), fmt.Sprintf("https://%s/login/oauth/access_token", domain)
}

func loadLegacyCopilotToken() (string, error) {
	tokens := loadLegacyCopilotTokens()
	if len(tokens) == 0 {
		return "", fmt.Errorf("legacy github-copilot token not found")
	}
	return tokens[0], nil
}

// loadLegacyCopilotTokens returns every editor-issued Copilot OAuth token found
// in ~/.config/github-copilot. Editors register several app ids there and only
// some of them can be exchanged for a Copilot API token, so all are returned.
func loadLegacyCopilotTokens() []string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}

	var tokens []string
	seen := map[string]bool{}
	for _, fileName := range []string{"hosts.json", "apps.json"} {
		filePath := filepath.Join(configDir, "github-copilot", fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var payload map[string]map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		hosts := make([]string, 0, len(payload))
		for host := range payload {
			hosts = append(hosts, host)
		}
		// Map iteration order is random; sort so token priority is stable.
		slices.Sort(hosts)
		for _, host := range hosts {
			if !strings.Contains(host, "github.com") {
				continue
			}
			token, ok := payload[host]["oauth_token"].(string)
			if !ok {
				continue
			}
			token = strings.TrimSpace(token)
			if token == "" || seen[token] {
				continue
			}
			seen[token] = true
			tokens = append(tokens, token)
		}
	}

	return tokens
}

func loadGitHubCLIToken() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(configDir, "gh", "hosts.yml"))
	if err != nil {
		return "", err
	}

	var hosts map[string]struct {
		OAuthToken string `yaml:"oauth_token"`
	}
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		return "", fmt.Errorf("parse gh hosts.yml: %w", err)
	}
	for _, host := range []string{resolveGitHubDomain(""), copilotDefaultGitHubDomain} {
		if entry, ok := hosts[host]; ok && strings.TrimSpace(entry.OAuthToken) != "" {
			return entry.OAuthToken, nil
		}
	}
	return "", fmt.Errorf("GitHub CLI token not found")
}

func sleepWithContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
