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
	ClaudeManualRedirectURL = "https://platform.claude.com/oauth/code/callback"
	ClaudeSuccessURL        = "https://claude.ai/oauth/code/success?app=claude-code"
	ClaudeOAuthScopes       = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	claudeCredentialFile     = "claude.json"
	claudeCodeCredentialFile = ".credentials.json"
	claudeCallbackTimeout    = 5 * time.Minute
)

type ClaudeCredentials struct {
	ClaudeAiOauth    *ClaudeOAuthToken `json:"claudeAiOauth"`
	OrganizationUUID string            `json:"organizationUuid,omitempty"`
}

type ClaudeOAuthToken struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken,omitempty"`
	ExpiresAt        int64    `json:"expiresAt"`
	Scopes           []string `json:"scopes,omitempty"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

type ClaudeProfile struct {
	Account      ClaudeProfileAccount      `json:"account"`
	Organization ClaudeProfileOrganization `json:"organization"`
}

type ClaudeProfileAccount struct {
	DisplayName  string `json:"display_name"`
	EmailAddress string `json:"email_address"`
	CreatedAt    string `json:"created_at"`
}

type ClaudeProfileOrganization struct {
	UUID                    string `json:"uuid"`
	OrganizationType        string `json:"organization_type"`
	RateLimitTier           string `json:"rate_limit_tier"`
	HasExtraUsageEnabled    bool   `json:"has_extra_usage_enabled"`
	BillingType             string `json:"billing_type"`
	SubscriptionCreatedAt   string `json:"subscription_created_at"`
}

type ClaudeAuthStatus struct {
	Authenticated    bool
	AccessToken      string
	SubscriptionType string
	DisplayName      string
	Email            string
	Source           string
}

type claudeTokenResponse struct {
	AccessToken      string   `json:"access_token"`
	RefreshToken     string   `json:"refresh_token,omitempty"`
	ExpiresIn        int64    `json:"expires_in,omitempty"`
	Scope            string   `json:"scope,omitempty"`
	TokenType        string   `json:"token_type,omitempty"`
	SubscriptionType string   `json:"subscription_type,omitempty"`
	RateLimitTier    string   `json:"rate_limit_tier,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	Error            string   `json:"error,omitempty"`
	ErrorDescription string   `json:"error_description,omitempty"`
}

func claudeCredentialFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "pando", "auth", claudeCredentialFile), nil
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func computeCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func RefreshOAuthToken(refreshToken string, scopes []string) (*ClaudeOAuthToken, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", ClaudeClientID)
	data.Set("scope", strings.Join(scopes, " "))

	resp, err := http.PostForm(ClaudeTokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token failed with status: %d", resp.StatusCode)
	}

	var tokenResp claudeTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	if tokenResp.Error != "" {
		return nil, fmt.Errorf("oauth error: %s - %s", tokenResp.Error, tokenResp.ErrorDescription)
	}

	return &ClaudeOAuthToken{
		AccessToken:      tokenResp.AccessToken,
		RefreshToken:     tokenResp.RefreshToken,
		ExpiresAt:        time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UnixMilli(),
		Scopes:           tokenResp.Scopes,
		SubscriptionType: tokenResp.SubscriptionType,
		RateLimitTier:    tokenResp.RateLimitTier,
	}, nil
}

func InstallOAuthTokens(tokens *ClaudeOAuthToken) error {
	path, err := claudeCredentialFilePath()
	if err != nil {
		return err
	}

	creds := ClaudeCredentials{
		ClaudeAiOauth: tokens,
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

type ClaudeLoginSession struct {
	ManualURL       string
	AutoURL         string
	AutoCodeCh      <-chan ClaudeAutoCode
	AutoRedirectURI string
	verifier        string
	state           string
	cancel          context.CancelFunc
}

type ClaudeAutoCode struct {
	Code        string
	RedirectURI string
	Err         error
}

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

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	autoRedirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

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

func (s *ClaudeLoginSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ClaudeLoginSession) ExtractCodeFromInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if strings.HasPrefix(input, "http") {
		if u, err := url.Parse(input); err == nil {
			q := u.Query()
			if st := q.Get("state"); st != "" && st != s.state {
				return ""
			}
			if code := q.Get("code"); code != "" {
				return code
			}
		}
	}
	if len(input) >= 10 && !strings.Contains(input, " ") {
		return input
	}
	return ""
}

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
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("create claude-code directory: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return fmt.Errorf("write claude-code credentials: %w", err)
	}
	return nil
}

func LoadClaudeCredentials() (*ClaudeCredentials, string, error) {
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		creds := &ClaudeCredentials{
			ClaudeAiOauth: &ClaudeOAuthToken{
				AccessToken: token,
			},
		}
		return creds, "env", nil
	}
	
	homeDir, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(homeDir, ".claude", claudeCodeCredentialFile)
		if data, err := os.ReadFile(path); err == nil {
			var creds ClaudeCredentials
			if err := json.Unmarshal(data, &creds); err == nil {
				return &creds, "claude-code", nil
			}
		}
	}

	path, err := claudeCredentialFilePath()
	if err == nil {
		if data, err := os.ReadFile(path); err == nil {
			var creds ClaudeCredentials
			if err := json.Unmarshal(data, &creds); err == nil {
				return &creds, "pando", nil
			}
		}
	}
	return nil, "none", fmt.Errorf("no credentials found")
}

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

func GetClaudeAuthStatus() (*ClaudeAuthStatus, error) {
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

	if profile, profileErr := GetClaudeProfile(creds.ClaudeAiOauth.AccessToken); profileErr == nil && profile != nil {
		status.DisplayName = profile.Account.DisplayName
		status.Email = profile.Account.EmailAddress
	}

	return status, nil
}

func GetClaudeToken(creds *ClaudeCredentials) (string, *ClaudeCredentials, error) {
	if creds == nil || creds.ClaudeAiOauth == nil {
		return "", nil, fmt.Errorf("credentials cannot be nil")
	}
	if time.Now().UnixMilli() > creds.ClaudeAiOauth.ExpiresAt {
		newCreds, err := RefreshOAuthToken(creds.ClaudeAiOauth.RefreshToken, creds.ClaudeAiOauth.Scopes)
		if err != nil {
			return "", nil, err
		}
		creds.ClaudeAiOauth = newCreds
		SaveClaudeCredentials(creds)
		SaveClaudeCodeCredentials(creds)
		return creds.ClaudeAiOauth.AccessToken, creds, nil
	}
	return creds.ClaudeAiOauth.AccessToken, nil, nil
}

func ClaudeLogout() error {
	path, err := claudeCredentialFilePath()
	if err != nil {
		return err
	}
	os.Remove(path)

	homeDir, _ := os.UserHomeDir()
	os.Remove(filepath.Join(homeDir, ".claude", claudeCodeCredentialFile))
	return nil
}
