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
	ClaudeClientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	ClaudeAuthorizeURL    = "https://claude.ai/oauth/authorize"
	ClaudeTokenURL        = "https://platform.claude.com/v1/oauth/token"
	ClaudeProfileURL      = "https://api.anthropic.com/api/oauth/profile"
	ClaudeOAuthBetaHeader = "oauth-2025-04-20"

	// ClaudeManualRedirectURL is used when the user cannot receive the automatic browser callback.
	// Matches claude-code's MANUAL_REDIRECT_URL = platform.claude.com/oauth/code/callback.
	// When used, platform.claude.com shows the authorization code to the user so they can paste it.
	ClaudeManualRedirectURL = "https://platform.claude.com/oauth/code/callback"

	// ClaudeSuccessURL is where to redirect after a successful automatic callback.
	// Matches claude-code's CLAUDEAI_SUCCESS_URL.
	ClaudeSuccessURL = "https://claude.ai/oauth/code/success?app=claude-code"

	// ClaudeOAuthScopes matches claude-code's ed1 scope set (union of console + claude.ai scopes).
	// org:create_api_key is required by the claude.ai authorization endpoint.
	ClaudeOAuthScopes = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"

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

// ClaudeProfile holds the user profile from Claude API (/api/oauth/profile).
// The response is nested: account and organization sub-objects.
type ClaudeProfile struct {
	Account      ClaudeProfileAccount      `json:"account"`
	Organization ClaudeProfileOrganization `json:"organization"`
}

// ClaudeProfileAccount holds account-level info from the profile response.
type ClaudeProfileAccount struct {
	DisplayName  string `json:"display_name"`
	EmailAddress string `json:"email_address"`
	CreatedAt    string `json:"created_at"`
}

// ClaudeProfileOrganization holds organization-level info from the profile response.
type ClaudeProfileOrganization struct {
	UUID                    string `json:"uuid"`
	OrganizationType        string `json:"organization_type"`
	RateLimitTier           string `json:"rate_limit_tier"`
	HasExtraUsageEnabled    bool   `json:"has_extra_usage_enabled"`
	BillingType             string `json:"billing_type"`
	SubscriptionCreatedAt   string `json:"subscription_created_at"`
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
// Uses 32 bytes like claude-code's hW4() function.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ClaudeLoginSession holds the state of an in-progress OAuth login flow.
// It is created by ClaudeLoginStart and consumed by ClaudeLoginFinish.
// The TUI layer owns the session and drives the two phases:
//  1. Display ManualURL to the user; open AutoURL in the browser.
//  2. Wait for AutoCodeCh (browser callback) or accept manual code from the dialog.
//  3. Call ClaudeLoginFinish with the received code and the appropriate redirect URI.
type ClaudeLoginSession struct {
	// ManualURL is the authorize URL using platform.claude.com as redirect_uri.
	// The user can open it manually; that page shows the authorization code.
	ManualURL string

	// AutoURL is the authorize URL using the local callback server as redirect_uri.
	// It is opened in the browser automatically.
	AutoURL string

	// AutoCodeCh receives the authorization code when the browser completes the flow.
	// The message carries the code and the redirect URI that was used.
	AutoCodeCh <-chan ClaudeAutoCode

	// AutoRedirectURI is the localhost redirect_uri for the automatic flow.
	AutoRedirectURI string

	// internal fields needed by ClaudeLoginFinish.
	verifier string
	state    string
	cancel   context.CancelFunc
}

// ClaudeAutoCode is sent on AutoCodeCh when the browser delivers the auth code.
type ClaudeAutoCode struct {
	Code        string
	RedirectURI string
	Err         error
}

// ClaudeLoginStart initializes the PKCE OAuth2 flow.
// It starts a local HTTP callback server (non-blocking) and returns a session
// with both the manual and automatic authorization URLs.
// The caller must eventually call session.Cancel() to release resources.
func ClaudeLoginStart() (*ClaudeLoginSession, error) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, err
	}
	challenge := computeCodeChallenge(verifier)

	state, err := generateState()
	if err != nil {
		return nil, err
	}

	// Start local callback server on a random port.
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	autoRedirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// buildAuthURL constructs the authorize URL with the given redirect_uri.
	// "code=true" is a non-standard param required by Anthropic (matches claude-code's GZ1).
	buildAuthURL := func(redirectURI string) string {
		params := url.Values{}
		params.Set("code", "true")
		params.Set("client_id", ClaudeClientID)
		params.Set("response_type", "code")
		params.Set("redirect_uri", redirectURI)
		params.Set("scope", ClaudeOAuthScopes)
		params.Set("code_challenge", challenge)
		params.Set("code_challenge_method", "S256")
		params.Set("state", state)
		return ClaudeAuthorizeURL + "?" + params.Encode()
	}

	autoCodeCh := make(chan ClaudeAutoCode, 1)

	ctx, cancel := context.WithTimeout(context.Background(), claudeCallbackTimeout)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		send := func(result ClaudeAutoCode) {
			select {
			case autoCodeCh <- result:
			default:
			}
		}
		if q.Get("state") != state {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			send(ClaudeAutoCode{Err: fmt.Errorf("invalid state parameter in callback")})
			return
		}
		if errParam := q.Get("error"); errParam != "" {
			http.Error(w, "Authorization failed: "+errParam, http.StatusBadRequest)
			send(ClaudeAutoCode{Err: fmt.Errorf("authorization error: %s: %s", errParam, q.Get("error_description"))})
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			send(ClaudeAutoCode{Err: fmt.Errorf("missing authorization code in callback")})
			return
		}
		http.Redirect(w, r, ClaudeSuccessURL, http.StatusFound)
		send(ClaudeAutoCode{Code: code, RedirectURI: autoRedirectURI})
	})

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()

	// Shut down the server when the context is done.
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	session := &ClaudeLoginSession{
		ManualURL:       buildAuthURL(ClaudeManualRedirectURL),
		AutoURL:         buildAuthURL(autoRedirectURI),
		AutoCodeCh:      autoCodeCh,
		AutoRedirectURI: autoRedirectURI,
		verifier:        verifier,
		state:           state,
		cancel:          cancel,
	}
	return session, nil
}

// Cancel releases the resources held by the session (stops the callback server).
func (s *ClaudeLoginSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

// ExtractCodeFromInput extracts the authorization code from text entered by the user.
// Accepts either a raw code string or a full redirect URL containing the code.
func (s *ClaudeLoginSession) ExtractCodeFromInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if strings.HasPrefix(input, "http") {
		if u, err := url.Parse(input); err == nil {
			q := u.Query()
			// Reject if state is present and mismatches.
			if st := q.Get("state"); st != "" && st != s.state {
				return ""
			}
			if code := q.Get("code"); code != "" {
				return code
			}
		}
	}
	// Raw code: non-empty, no spaces, reasonable length.
	if len(input) >= 10 && !strings.Contains(input, " ") {
		return input
	}
	return ""
}

// ClaudeLoginFinish exchanges an authorization code for credentials and fetches
// the user profile. redirectURI must be the one actually used during authorization
// (either session.AutoRedirectURI or ClaudeManualRedirectURL).
func ClaudeLoginFinish(session *ClaudeLoginSession, code, redirectURI string) (*ClaudeCredentials, string, error) {
	defer session.Cancel()

	tokenResp, err := exchangeClaudeCode(code, redirectURI, session.verifier, session.state)
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

	// Fetch profile — matches claude-code's wc6 post-login handler calling Kg(accessToken).
	displayName := ""
	profile, err := GetClaudeProfile(tokenResp.AccessToken)
	if err == nil && profile != nil {
		displayName = profile.Account.DisplayName
		if profile.Organization.UUID != "" {
			creds.OrganizationUUID = profile.Organization.UUID
		}
		if creds.ClaudeAiOauth.SubscriptionType == "" {
			creds.ClaudeAiOauth.SubscriptionType = subscriptionTypeFromOrg(profile.Organization.OrganizationType)
		}
		if creds.ClaudeAiOauth.RateLimitTier == "" {
			creds.ClaudeAiOauth.RateLimitTier = profile.Organization.RateLimitTier
		}
	}

	return creds, displayName, nil
}

// subscriptionTypeFromOrg maps the organization_type field to a friendly subscription label,
// matching the logic in claude-code's fZ1 function.
func subscriptionTypeFromOrg(orgType string) string {
	switch orgType {
	case "claude_max":
		return "max"
	case "claude_pro":
		return "pro"
	case "claude_enterprise":
		return "enterprise"
	case "claude_team":
		return "team"
	default:
		return ""
	}
}

// exchangeClaudeCode exchanges an authorization code for tokens.
func exchangeClaudeCode(code, redirectURI, verifier, state string) (*claudeTokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURI,
		"client_id":     ClaudeClientID,
		"code_verifier": verifier,
		"state":         state,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal token exchange payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, ClaudeTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create token exchange request: %w", err)
	}
	// Only Content-Type is sent — matches claude-code's by8 function (no anthropic-beta header).
	req.Header.Set("Content-Type", "application/json")

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

// SaveClaudeCodeCredentials writes refreshed credentials back to Claude Code's own
// credential file (~/.claude/.credentials.json), keeping both tools in sync.
func SaveClaudeCodeCredentials(creds *ClaudeCredentials) error {
	if creds == nil || creds.ClaudeAiOauth == nil {
		return fmt.Errorf("credentials cannot be nil")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	filePath := filepath.Join(homeDir, ".claude", claudeCodeCredentialFile)
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude credentials: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return fmt.Errorf("write claude-code credentials: %w", err)
	}
	return nil
}

// LoadClaudeCredentials loads Claude credentials with the following priority:
//  1. CLAUDE_CODE_OAUTH_TOKEN env var → synthetic credentials
//  2. ANTHROPIC_API_KEY env var → nil (API key mode)
//  3. ~/.claude/.credentials.json (preferred read-only source from Claude Code)
//  4. ~/.config/pando/auth/claude.json (pando's own store fallback)
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

	// 3. Try Claude Code's own credentials file (~/.claude/.credentials.json).
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

	// 4. Try pando's own credential file.
	pandoPath, err := claudeCredentialFilePath()
	if err == nil {
		if data, readErr := os.ReadFile(pandoPath); readErr == nil {
			var creds ClaudeCredentials
			if jsonErr := json.Unmarshal(data, &creds); jsonErr == nil && creds.ClaudeAiOauth != nil && creds.ClaudeAiOauth.AccessToken != "" {
				return &creds, "pando", nil
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
	// Only Content-Type is sent — matches claude-code's QQ6 function (no anthropic-beta header).
	req.Header.Set("Content-Type", "application/json")

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
// Matches claude-code's Kg function: only Authorization and Content-Type headers.
func GetClaudeProfile(accessToken string) (*ClaudeProfile, error) {
	req, err := http.NewRequest(http.MethodGet, ClaudeProfileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

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
		status.DisplayName = profile.Account.DisplayName
		status.Email = profile.Account.EmailAddress
	}

	return status, nil
}
