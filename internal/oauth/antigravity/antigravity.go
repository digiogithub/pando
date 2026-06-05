package antigravity

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	GoogleClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	GoogleClientSecret = "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"
	defaultHTTPTimeout = 15 * time.Second
)

var (
	GoogleAuthorizeURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL          = "https://oauth2.googleapis.com/token"
	GoogleUserInfoURL       = "https://openidconnect.googleapis.com/v1/userinfo"
	AntigravityLoadURLProd  = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	AntigravityLoadURLDaily = "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:loadCodeAssist"
)

var DefaultScopes = []string{
	"openid",
	"email",
	"profile",
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

type Session struct {
	State        string `json:"state"`
	CodeVerifier string `json:"codeVerifier"`
	RedirectURI  string `json:"redirectUri"`
	ProjectID    string `json:"projectId,omitempty"`
}

type Token struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken,omitempty"`
	TokenType    string   `json:"tokenType,omitempty"`
	Expiry       int64    `json:"expiry,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	IDToken      string   `json:"idToken,omitempty"`
	ProjectID    string   `json:"projectId,omitempty"`
}

type UserInfo struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Subject       string `json:"sub"`
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	ExpiresIn        int64  `json:"expires_in,omitempty"`
	Scope            string `json:"scope,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	IDToken          string `json:"id_token,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

type projectsEnvelope struct {
	Projects []Project `json:"projects"`
}

func NewSession(redirectURI string) (*Session, error) {
	state, err := randomURLSafeString(32)
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	verifier, err := randomURLSafeString(32)
	if err != nil {
		return nil, fmt.Errorf("generate code verifier: %w", err)
	}
	return &Session{State: state, CodeVerifier: verifier, RedirectURI: strings.TrimSpace(redirectURI)}, nil
}

func (s Session) ValidateCallback(state string) error {
	if strings.TrimSpace(s.State) == "" {
		return fmt.Errorf("missing oauth state")
	}
	if state != s.State {
		return fmt.Errorf("invalid oauth state")
	}
	return nil
}

func (s Session) AuthURL() (string, error) {
	if strings.TrimSpace(s.RedirectURI) == "" {
		return "", fmt.Errorf("redirect URI is required")
	}
	if strings.TrimSpace(s.CodeVerifier) == "" {
		return "", fmt.Errorf("code verifier is required")
	}
	if strings.TrimSpace(s.State) == "" {
		return "", fmt.Errorf("state is required")
	}

	params := url.Values{}
	params.Set("client_id", GoogleClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", s.RedirectURI)
	params.Set("scope", strings.Join(DefaultScopes, " "))
	params.Set("state", s.State)
	params.Set("code_challenge", CodeChallenge(s.CodeVerifier))
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")

	return GoogleAuthorizeURL + "?" + params.Encode(), nil
}

func CodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func ExchangeCode(client *http.Client, code, verifier, redirectURI string) (*Token, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", GoogleClientID)
	values.Set("client_secret", GoogleClientSecret)
	values.Set("code", strings.TrimSpace(code))
	values.Set("code_verifier", strings.TrimSpace(verifier))
	values.Set("redirect_uri", strings.TrimSpace(redirectURI))
	return doTokenRequest(client, values)
}

func RefreshToken(client *http.Client, refreshToken string) (*Token, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("client_id", GoogleClientID)
	values.Set("client_secret", GoogleClientSecret)
	values.Set("refresh_token", strings.TrimSpace(refreshToken))
	return doTokenRequest(client, values)
}

func FetchUserInfo(client *http.Client, accessToken string) (*UserInfo, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("access token is required")
	}
	var info UserInfo
	if err := doJSONRequest(client, http.MethodGet, GoogleUserInfoURL, accessToken, nil, &info); err != nil {
		return nil, err
	}
	if strings.TrimSpace(info.Email) == "" {
		return nil, fmt.Errorf("google userinfo response missing email")
	}
	return &info, nil
}

type loadCodeAssistResponse struct {
	CloudaicompanionProject struct {
		ID string `json:"id,omitempty"`
	} `json:"cloudaicompanionProject,omitempty"`
}

func FetchProjectID(client *http.Client, accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("access token is required")
	}

	endpoints := []string{AntigravityLoadURLProd, AntigravityLoadURLDaily}
	for _, endpoint := range endpoints {
		var resp loadCodeAssistResponse
		body := bytes.NewBufferString(`{"metadata":{"ideType":"ANTIGRAVITY","platform":"MACOS","pluginType":"GEMINI"}}`)
		if err := doJSONRequest(client, http.MethodPost, endpoint, accessToken, body, &resp); err != nil {
			continue
		}
		if id := strings.TrimSpace(resp.CloudaicompanionProject.ID); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to resolve project ID from loadCodeAssist")
}

func httpClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func doTokenRequest(client *http.Client, values url.Values) (*Token, error) {
	if strings.TrimSpace(values.Get("client_id")) == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if strings.TrimSpace(values.Get("client_secret")) == "" {
		return nil, fmt.Errorf("client_secret is required")
	}
	if strings.TrimSpace(values.Get("grant_type")) == "authorization_code" {
		if strings.TrimSpace(values.Get("code")) == "" {
			return nil, fmt.Errorf("authorization code is required")
		}
		if strings.TrimSpace(values.Get("code_verifier")) == "" {
			return nil, fmt.Errorf("code verifier is required")
		}
		if strings.TrimSpace(values.Get("redirect_uri")) == "" {
			return nil, fmt.Errorf("redirect URI is required")
		}
	}
	if strings.TrimSpace(values.Get("grant_type")) == "refresh_token" && strings.TrimSpace(values.Get("refresh_token")) == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	req, err := http.NewRequest(http.MethodPost, GoogleTokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "google-api-nodejs-client/9.15.1")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")

	resp, err := httpClient(client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("oauth error: %s: %s", tokenResp.Error, tokenResp.ErrorDescription)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("token response missing access token")
	}

	token := &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scopes:       strings.Fields(tokenResp.Scope),
		IDToken:      tokenResp.IDToken,
	}
	if tokenResp.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Unix()
	}
	return token, nil
}

func doJSONRequest(client *http.Client, method, targetURL, accessToken string, body io.Reader, out any) error {
	req, err := http.NewRequest(method, targetURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient(client).Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("request %s failed with status %d: %s", targetURL, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func randomURLSafeString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
