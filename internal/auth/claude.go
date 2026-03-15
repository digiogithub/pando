package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ClaudeClientID           = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeAuthorizeURL       = "https://claude.ai/oauth/authorize"
	ClaudeTokenURL           = "https://platform.claude.com/v1/oauth/token"
	ClaudeProfileURL         = "https://api.anthropic.com/api/oauth/profile"
	ClaudeOAuthBetaHeader    = "oauth-2025-04-20"
	ClaudeOAuthScopes        = "user:file_upload user:inference user:mcp_servers user:profile user:sessions:claude_code"
	claudeCredentialFile     = "claude.json"
	claudeCodeCredentialFile = ".credentials.json"

	claudeCallbackTimeout = 5 * time.Minute
)

// ClaudeCredentials holds the OAuth credentials for Claude.
type ClaudeCredentials struct {
	ClaudeAiOauth    *ClaudeOAuthToken `json:"claudeAiOauth"`
	OrganizationUUID string            `json:"organizationUuid,omitempty"`
}

// ClaudeOAuthToken holds the OAuth token details.
type ClaudeOAuthToken struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken,omitempty"`
	ExpiresAt        int64    `json:"expiresAt"` // Unix milliseconds
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

// ClaudeProfile holds the user profile from Claude API.
type ClaudeProfile struct {
	DisplayName  string `json:"display_name"`
	EmailAddress string `json:"email_address"`
}

// ClaudeAuthStatus holds the authentication status for Claude.
type ClaudeAuthStatus struct {
	Authenticated    bool
	AccessToken      string
	SubscriptionType string
	DisplayName      string
	Email            string
	Source           string // "env", "pando", or "claude-code"
}

// claudeTokenResponse is the raw response from the token endpoint.
type claudeTokenResponse struct {
	AccessToken      string   `json:"access_token"`
	RefreshToken     string   `json:"refresh_token,omitempty"`
	ExpiresIn        int64    `json:"expires_in,omitempty"` // seconds
	Scope            string   `json:"scope,omitempty"`
	TokenType        string   `json:"token_type,omitempty"`
	SubscriptionType string   `json:"subscription_type,omitempty"`
	RateLimitTier    string   `json:"rate_limit_tier,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	Error            string   `json:"error,omitempty"`
	ErrorDescription string   `json:"error_description,omitempty"`
}

// claudeCredentialFilePath returns the path to pando's claude credential file.
func claudeCredentialFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "pando", "auth", claudeCredentialFile), nil
}

// generateCodeVerifier generates a random PKCE code verifier.
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// computeCodeChallenge computes the PKCE S256 code challenge from a verifier.
func computeCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// generateState generates a random state nonce.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ClaudeLogin performs the full PKCE OAuth2 flow for Claude.
// Returns credentials, display name, and any error.
func ClaudeLogin() (*ClaudeCredentials, string, error) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, "", err
	}
	challenge := computeCodeChallenge(verifier)

	state, err := generateState()
	if err != nil {
		return nil, "", err
	}

	// Start local HTTP server to receive the callback.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Build authorization URL.
	params := url.Values{}
	params.Set("client_id", ClaudeClientID)
	params.Set("response_type", "code")
	params.Set("scope", ClaudeOAuthScopes)
	params.Set("redirect_uri", redirectURI)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	authURL := ClaudeAuthorizeURL + "?" + params.Encode()

	// Open browser.
	if err := OpenBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open browser automatically. Please visit:\n%s\n", authURL)
	}

	// Wait for callback.
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			errCh <- fmt.Errorf("invalid state parameter in callback")
			return
		}
		if errParam := q.Get("error"); errParam != "" {
			desc := q.Get("error_description")
			http.Error(w, "Authorization failed: "+errParam, http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization error: %s: %s", errParam, desc)
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			errCh <- fmt.Errorf("missing authorization code in callback")
			return
		}
		fmt.Fprintln(w, "<html><body><h2>Authentication successful! You can close this tab.</h2></body></html>")
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server error: %w", serveErr)
		}
	}()
	defer srv.Shutdown(context.Background()) //nolint:errcheck

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return nil, "", err
	case <-time.After(claudeCallbackTimeout):
		return nil, "", fmt.Errorf("timed out waiting for OAuth callback (5 minutes)")
	}

	// Exchange code for tokens.
	tokenResp, err := exchangeClaudeCode(code, redirectURI, verifier)
	if err != nil {
		return nil, "", err
	}

	expiresAt := time.Now().UnixMilli() + tokenResp.ExpiresIn*1000
	scopes := tokenResp.Scopes
	if len(scopes) == 0 && tokenResp.Scope != "" {
		scopes = strings.Fields(tokenResp.Scope)
	}

	creds := &ClaudeCredentials{
		ClaudeAiOauth: &ClaudeOAuthToken{
			AccessToken:      tokenResp.AccessToken,
			RefreshToken:     tokenResp.RefreshToken,
			ExpiresAt:        expiresAt,
			Scopes:           scopes,
			SubscriptionType: tokenResp.SubscriptionType,
			RateLimitTier:    tokenResp.RateLimitTier,
		},
	}

	// Fetch profile.
	displayName := ""
	profile, err := GetClaudeProfile(tokenResp.AccessToken)
	if err == nil && profile != nil {
		displayName = profile.DisplayName
	}

	return creds, displayName, nil
}

// exchangeClaudeCode exchanges an authorization code for tokens.
func exchangeClaudeCode(code, redirectURI, verifier string) (*claudeTokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURI,
		"client_id":     ClaudeClientID,
		"code_verifier": verifier,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal token exchange payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, ClaudeTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", ClaudeOAuthBetaHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token exchange response: %w", err)
	}

	var tokenResp claudeTokenResponse
	if err := json.Unmarshal(data, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode token exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if tokenResp.Error != "" {
			return nil, fmt.Errorf("token exchange failed: %s: %s", tokenResp.Error, tokenResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token exchange failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token exchange returned empty access token")
	}
	return &tokenResp, nil
}

// SaveClaudeCredentials saves Claude credentials to pando's config directory.
func SaveClaudeCredentials(creds *ClaudeCredentials) error {
	if creds == nil || creds.ClaudeAiOauth == nil {
		return fmt.Errorf("credentials cannot be nil")
	}
	filePath, err := claudeCredentialFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create claude auth directory: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude credentials: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return fmt.Errorf("write claude credentials: %w", err)
	}
	return nil
}

// LoadClaudeCredentials loads Claude credentials with the following priority:
//  1. CLAUDE_CODE_OAUTH_TOKEN env var → synthetic credentials
//  2. ANTHROPIC_API_KEY env var → nil (API key mode)
//  3. ~/.config/pando/auth/claude.json (pando's own store)
//  4. ~/.claude/.credentials.json (read-only fallback from Claude Code)
//
// Returns (creds, source, error) where source is "env", "pando", or "claude-code".
func LoadClaudeCredentials() (*ClaudeCredentials, string, error) {
	// 1. Check env CLAUDE_CODE_OAUTH_TOKEN.
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		creds := &ClaudeCredentials{
			ClaudeAiOauth: &ClaudeOAuthToken{
				AccessToken: token,
				ExpiresAt:   time.Now().Add(24 * time.Hour).UnixMilli(), // assume valid for now
			},
		}
		return creds, "env", nil
	}

	// 2. Check ANTHROPIC_API_KEY — caller uses API key mode.
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
		return nil, "env", nil
	}

	// 3. Try pando's own credential file.
	pandoPath, err := claudeCredentialFilePath()
	if err == nil {
		if data, readErr := os.ReadFile(pandoPath); readErr == nil {
			var creds ClaudeCredentials
			if jsonErr := json.Unmarshal(data, &creds); jsonErr == nil && creds.ClaudeAiOauth != nil && creds.ClaudeAiOauth.AccessToken != "" {
				return &creds, "pando", nil
			}
		}
	}

	// 4. Try Claude Code's own credentials file (~/.claude/.credentials.json).
	homeDir, err := os.UserHomeDir()
	if err == nil {
		claudeCodePath := filepath.Join(homeDir, ".claude", claudeCodeCredentialFile)
		if data, readErr := os.ReadFile(claudeCodePath); readErr == nil {
			var creds ClaudeCredentials
			if jsonErr := json.Unmarshal(data, &creds); jsonErr == nil && creds.ClaudeAiOauth != nil && creds.ClaudeAiOauth.AccessToken != "" {
				return &creds, "claude-code", nil
			}
		}
	}

	return nil, "", fmt.Errorf("no Claude credentials found; run `pando auth claude login`")
}

// ClaudeLogout removes pando's Claude credential file.
func ClaudeLogout() error {
	filePath, err := claudeCredentialFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete claude credentials: %w", err)
	}
	return nil
}

// IsClaudeTokenExpired returns true if the token expires within the next 5 minutes.
func IsClaudeTokenExpired(creds *ClaudeCredentials) bool {
	if creds == nil || creds.ClaudeAiOauth == nil {
		return true
	}
	const bufferMs = 300_000 // 5 minutes in milliseconds
	return creds.ClaudeAiOauth.ExpiresAt-time.Now().UnixMilli() < bufferMs
}

// RefreshClaudeToken uses the refresh token to obtain a new access token.
func RefreshClaudeToken(creds *ClaudeCredentials) (*ClaudeCredentials, error) {
	if creds == nil || creds.ClaudeAiOauth == nil {
		return nil, fmt.Errorf("credentials are nil")
	}
	if creds.ClaudeAiOauth.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	payload := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": creds.ClaudeAiOauth.RefreshToken,
		"client_id":     ClaudeClientID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal refresh token payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, ClaudeTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create refresh token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", ClaudeOAuthBetaHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh token response: %w", err)
	}

	var tokenResp claudeTokenResponse
	if err := json.Unmarshal(data, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if tokenResp.Error != "" {
			return nil, fmt.Errorf("refresh token failed: %s: %s", tokenResp.Error, tokenResp.ErrorDescription)
		}
		return nil, fmt.Errorf("refresh token failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("refresh token returned empty access token")
	}

	expiresAt := time.Now().UnixMilli() + tokenResp.ExpiresIn*1000
	scopes := tokenResp.Scopes
	if len(scopes) == 0 && tokenResp.Scope != "" {
		scopes = strings.Fields(tokenResp.Scope)
	}
	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = creds.ClaudeAiOauth.RefreshToken
	}

	updated := &ClaudeCredentials{
		ClaudeAiOauth: &ClaudeOAuthToken{
			AccessToken:      tokenResp.AccessToken,
			RefreshToken:     refreshToken,
			ExpiresAt:        expiresAt,
			Scopes:           scopes,
			SubscriptionType: tokenResp.SubscriptionType,
			RateLimitTier:    tokenResp.RateLimitTier,
		},
		OrganizationUUID: creds.OrganizationUUID,
	}
	return updated, nil
}

// GetValidClaudeToken returns a valid access token, refreshing if needed.
// Returns (accessToken, updatedCreds, error); updatedCreds is non-nil only when refreshed.
func GetValidClaudeToken(creds *ClaudeCredentials) (string, *ClaudeCredentials, error) {
	if creds == nil || creds.ClaudeAiOauth == nil {
		return "", nil, fmt.Errorf("credentials are nil")
	}
	if !IsClaudeTokenExpired(creds) {
		return creds.ClaudeAiOauth.AccessToken, nil, nil
	}
	refreshed, err := RefreshClaudeToken(creds)
	if err != nil {
		return "", nil, fmt.Errorf("refresh Claude token: %w", err)
	}
	return refreshed.ClaudeAiOauth.AccessToken, refreshed, nil
}

// GetClaudeProfile fetches the user profile from the Claude API.
func GetClaudeProfile(accessToken string) (*ClaudeProfile, error) {
	req, err := http.NewRequest(http.MethodGet, ClaudeProfileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", ClaudeOAuthBetaHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch claude profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch claude profile failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var profile ClaudeProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode claude profile: %w", err)
	}
	return &profile, nil
}

// GetClaudeAuthStatus loads credentials and returns the current authentication status.
func GetClaudeAuthStatus() (*ClaudeAuthStatus, error) {
	// Check for API key mode first.
	if apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); apiKey != "" {
		return &ClaudeAuthStatus{
			Authenticated: true,
			AccessToken:   apiKey,
			Source:        "env",
		}, nil
	}

	creds, source, err := LoadClaudeCredentials()
	if err != nil {
		return &ClaudeAuthStatus{
			Authenticated: false,
			Source:        "none",
		}, err
	}
	if creds == nil || creds.ClaudeAiOauth == nil {
		return &ClaudeAuthStatus{
			Authenticated: false,
			Source:        source,
		}, nil
	}

	status := &ClaudeAuthStatus{
		Authenticated:    true,
		AccessToken:      creds.ClaudeAiOauth.AccessToken,
		SubscriptionType: creds.ClaudeAiOauth.SubscriptionType,
		Source:           source,
	}

	// Try to fetch profile for display name / email (best-effort).
	if profile, profileErr := GetClaudeProfile(creds.ClaudeAiOauth.AccessToken); profileErr == nil && profile != nil {
		status.DisplayName = profile.DisplayName
		status.Email = profile.EmailAddress
	}

	return status, nil
}
