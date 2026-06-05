package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	antigravity "github.com/digiogithub/pando/internal/oauth/antigravity"
)

func resetProviderAccountsTestConfig(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".pando.json")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	config.SetForTests(&config.Config{
		WorkingDir:       tmpDir,
		ProviderAccounts: nil,
		Providers:        map[models.ModelProvider]config.Provider{},
		MCPServers:       map[string]config.MCPServer{},
		LSP:              map[string]config.LSPConfig{},
	})
}

func TestProviderTypesIncludesAntigravityOAuthMetadata(t *testing.T) {
	for _, provider := range providerTypes {
		if provider.Type != "antigravity" {
			continue
		}
		if provider.DisplayName != "Antigravity" {
			t.Fatalf("display name = %q, want %q", provider.DisplayName, "Antigravity")
		}
		if provider.RequiresAPIKey {
			t.Fatal("expected antigravity provider type not to require API key")
		}
		if !provider.SupportsOAuth {
			t.Fatal("expected antigravity provider type to support OAuth")
		}
		return
	}

	t.Fatal("expected antigravity provider type metadata to be present")
}

func TestProviderAccountToResponseRemovesOAuthSecretsAndMasksAPIKey(t *testing.T) {
	response := providerAccountToResponse(config.ProviderAccount{
		ID:                "antigravity-work",
		Type:              "antigravity",
		APIKey:            "secret-api-key",
		OAuthRefreshToken: "refresh-secret",
		OAuthAccessToken:  "access-secret",
		OAuthExpiry:       1735689600,
		Email:             "dev@example.com",
	}, true)

	if response.APIKey != "***-key" {
		t.Fatalf("api key = %q, want %q", response.APIKey, "***-key")
	}
	if response.OAuthRefreshToken != "" {
		t.Fatal("expected oauth refresh token to be removed from API response")
	}
	if response.OAuthAccessToken != "" {
		t.Fatal("expected oauth access token to be removed from API response")
	}
	if response.OAuthExpiry != 1735689600 {
		t.Fatalf("oauth expiry = %d, want %d", response.OAuthExpiry, int64(1735689600))
	}
	if response.Email != "dev@example.com" {
		t.Fatalf("email = %q, want %q", response.Email, "dev@example.com")
	}
}

func TestHandleGetProviderAccountSanitizesOAuthSecrets(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:                "antigravity-work",
		DisplayName:       "Antigravity Work",
		Type:              "antigravity",
		OAuthRefreshToken: "refresh-secret",
		OAuthAccessToken:  "access-secret",
		OAuthExpiry:       1735689600,
		Email:             "dev@example.com",
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/provider-accounts/antigravity-work", nil)
	req.SetPathValue("id", "antigravity-work")
	rec := httptest.NewRecorder()

	var server Server
	server.handleGetProviderAccount(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got config.ProviderAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.OAuthRefreshToken != "" {
		t.Fatal("expected oauth refresh token to be omitted from handler response")
	}
	if got.OAuthAccessToken != "" {
		t.Fatal("expected oauth access token to be omitted from handler response")
	}
	if got.OAuthExpiry != account.OAuthExpiry {
		t.Fatalf("oauth expiry = %d, want %d", got.OAuthExpiry, account.OAuthExpiry)
	}
	if got.Email != account.Email {
		t.Fatalf("email = %q, want %q", got.Email, account.Email)
	}
}

func TestAntigravityTokenNeedsRefresh(t *testing.T) {
	if !provider.AntigravityTokenNeedsRefresh(config.ProviderAccount{}) {
		t.Fatal("expected empty account to need refresh")
	}
	if provider.AntigravityTokenNeedsRefresh(config.ProviderAccount{OAuthAccessToken: "token", OAuthExpiry: time.Now().Add(5 * time.Minute).Unix()}) {
		t.Fatal("expected fresh token not to need refresh")
	}
	if !provider.AntigravityTokenNeedsRefresh(config.ProviderAccount{OAuthAccessToken: "token", OAuthExpiry: time.Now().Add(5 * time.Second).Unix()}) {
		t.Fatal("expected near-expiry token to need refresh")
	}
}

func TestHandleAntigravityOAuthStartCreatesPendingAccount(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	body := strings.NewReader(`{"accountId":"antigravity-work","displayName":"Antigravity Work","redirectUri":"http://localhost:7777/callback"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity/start", body)
	rec := httptest.NewRecorder()

	server := Server{config: ServerConfig{Host: "127.0.0.1", Port: 7777}}
	server.handleAntigravityOAuthStart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp antigravityOAuthStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AuthURL == "" {
		t.Fatal("expected auth URL")
	}
	account, ok := config.GetProviderAccount("antigravity-work")
	if !ok {
		t.Fatal("expected provider account to be created")
	}
	if account.Type != "antigravity" {
		t.Fatalf("account type = %q, want antigravity", account.Type)
	}
	if account.OAuthState == "" || account.OAuthCodeVerifier == "" {
		t.Fatal("expected oauth state and verifier to be stored")
	}
}

func TestHandleAntigravityOAuthCallbackPersistsTokens(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:                "antigravity-work",
		DisplayName:       "Antigravity Work",
		Type:              "antigravity",
		UseOAuth:          true,
		OAuthState:        "state-123",
		OAuthCodeVerifier: "verifier-123",
		OAuthRedirectURI:  "http://localhost/callback",
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	oldTokenURL := antigravity.GoogleTokenURL
	oldUserInfoURL := antigravity.GoogleUserInfoURL
	oldProjectsURL := antigravity.AntigravityLoadURLProd
	defer func() {
		antigravity.GoogleTokenURL = oldTokenURL
		antigravity.GoogleUserInfoURL = oldUserInfoURL
		antigravity.AntigravityLoadURLProd = oldProjectsURL
	}()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm(): %v", err)
		}
		if r.Form.Get("code") != "auth-code" {
			t.Fatalf("code = %q, want auth-code", r.Form.Get("code"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-123",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()
	antigravity.GoogleTokenURL = tokenServer.URL

	userServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "dev@example.com", "email_verified": true})
	}))
	defer userServer.Close()
	antigravity.GoogleUserInfoURL = userServer.URL

	projectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": map[string]string{"id": "project-123"}})
	}))
	defer projectServer.Close()
	antigravity.AntigravityLoadURLProd = projectServer.URL

	body := strings.NewReader(`{"accountId":"antigravity-work","code":"auth-code","state":"state-123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity/callback", body)
	rec := httptest.NewRecorder()

	var server Server
	server.handleAntigravityOAuthCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	updated, ok := config.GetProviderAccount("antigravity-work")
	if !ok {
		t.Fatal("expected updated provider account")
	}
	if updated.OAuthAccessToken != "access-123" {
		t.Fatalf("access token = %q, want access-123", updated.OAuthAccessToken)
	}
	if updated.OAuthRefreshToken != "refresh-123" {
		t.Fatalf("refresh token = %q, want refresh-123", updated.OAuthRefreshToken)
	}
	if updated.Email != "dev@example.com" {
		t.Fatalf("email = %q, want dev@example.com", updated.Email)
	}
	if updated.ProjectID != "project-123" {
		t.Fatalf("projectID = %q, want project-123", updated.ProjectID)
	}
}

func TestHandleAntigravityOAuthVerifyReturnsDisconnectedWithoutTokens(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:          "antigravity-empty",
		DisplayName: "Antigravity Empty",
		Type:        "antigravity",
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	body := strings.NewReader(`{"accountId":"antigravity-empty"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity/verify", body)
	rec := httptest.NewRecorder()

	var server Server
	server.handleAntigravityOAuthVerify(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp antigravityOAuthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Connected {
		t.Fatal("expected disconnected response")
	}
	if resp.AccountID != "antigravity-empty" {
		t.Fatalf("account ID = %q, want %q", resp.AccountID, "antigravity-empty")
	}
}

func TestHandleAntigravityOAuthStartUsesDefaultRedirectAndMergesMetadata(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:          "antigravity-work",
		DisplayName: "Old Name",
		Type:        "antigravity",
		ExtraHeaders: map[string]string{
			"existing": "value",
		},
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	body := strings.NewReader(`{"accountId":"antigravity-work","displayName":"Antigravity Work","callbackPath":"oauth/callback"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity/start", body)
	rec := httptest.NewRecorder()

	server := Server{config: ServerConfig{Host: "0.0.0.0", Port: 7777}}
	server.handleAntigravityOAuthStart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, ok := config.GetProviderAccount("antigravity-work")
	if !ok {
		t.Fatal("expected provider account to exist")
	}
	if updated.DisplayName != "Antigravity Work" {
		t.Fatalf("display name = %q, want %q", updated.DisplayName, "Antigravity Work")
	}
	if updated.ExtraHeaders["existing"] != "value" {
		t.Fatalf("existing metadata = %q, want value", updated.ExtraHeaders["existing"])
	}
	if updated.OAuthRedirectURI != "http://127.0.0.1:7777/oauth/callback" {
		t.Fatalf("redirect URI metadata = %q", updated.OAuthRedirectURI)
	}
}

func TestHandleAntigravityOAuthCallbackReusesExistingRefreshToken(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:                "antigravity-work",
		DisplayName:       "Antigravity Work",
		Type:              "antigravity",
		UseOAuth:          true,
		OAuthRefreshToken: "existing-refresh",
		OAuthState:        "state-123",
		OAuthCodeVerifier: "verifier-123",
		OAuthRedirectURI:  "http://localhost/callback",
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	oldTokenURL := antigravity.GoogleTokenURL
	oldUserInfoURL := antigravity.GoogleUserInfoURL
	oldProjectsURL := antigravity.AntigravityLoadURLProd
	defer func() {
		antigravity.GoogleTokenURL = oldTokenURL
		antigravity.GoogleUserInfoURL = oldUserInfoURL
		antigravity.AntigravityLoadURLProd = oldProjectsURL
	}()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-123",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()
	antigravity.GoogleTokenURL = tokenServer.URL

	userServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "dev@example.com", "email_verified": true})
	}))
	defer userServer.Close()
	antigravity.GoogleUserInfoURL = userServer.URL

	projectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": map[string]string{"id": "project-123"}})
	}))
	defer projectServer.Close()
	antigravity.AntigravityLoadURLProd = projectServer.URL

	body := strings.NewReader(`{"accountId":"antigravity-work","code":"auth-code","state":"state-123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity/callback", body)
	rec := httptest.NewRecorder()

	var server Server
	server.handleAntigravityOAuthCallback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, ok := config.GetProviderAccount("antigravity-work")
	if !ok {
		t.Fatal("expected updated provider account")
	}
	if updated.OAuthRefreshToken != "existing-refresh" {
		t.Fatalf("refresh token = %q, want %q", updated.OAuthRefreshToken, "existing-refresh")
	}
	if updated.OAuthState != "" || updated.OAuthCodeVerifier != "" {
		t.Fatal("expected oauth session metadata to be cleared")
	}
	if updated.OAuthRedirectURI != "http://localhost/callback" {
		t.Fatalf("redirect URI metadata = %q, want preserved callback", updated.OAuthRedirectURI)
	}
}

func TestHandleAntigravityOAuthRefreshRefreshesExpiredAccount(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:                "antigravity-work",
		DisplayName:       "Antigravity Work",
		Type:              "antigravity",
		UseOAuth:          true,
		OAuthAccessToken:  "stale-access",
		OAuthRefreshToken: "refresh-123",
		OAuthExpiry:       time.Now().Add(-1 * time.Minute).Unix(),
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	oldTokenURL := antigravity.GoogleTokenURL
	oldUserInfoURL := antigravity.GoogleUserInfoURL
	oldProjectsURL := antigravity.AntigravityLoadURLProd
	defer func() {
		antigravity.GoogleTokenURL = oldTokenURL
		antigravity.GoogleUserInfoURL = oldUserInfoURL
		antigravity.AntigravityLoadURLProd = oldProjectsURL
	}()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm(): %v", err)
		}
		if r.Form.Get("refresh_token") != "refresh-123" {
			t.Fatalf("refresh token = %q, want refresh-123", r.Form.Get("refresh_token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()
	antigravity.GoogleTokenURL = tokenServer.URL

	userServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"email": "dev@example.com", "email_verified": true})
	}))
	defer userServer.Close()
	antigravity.GoogleUserInfoURL = userServer.URL

	projectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": map[string]string{"id": "project-123"}})
	}))
	defer projectServer.Close()
	antigravity.AntigravityLoadURLProd = projectServer.URL

	body := strings.NewReader(`{"accountId":"antigravity-work","force":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity/refresh", body)
	rec := httptest.NewRecorder()

	var server Server
	server.handleAntigravityOAuthRefresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	updated, ok := config.GetProviderAccount("antigravity-work")
	if !ok {
		t.Fatal("expected refreshed provider account")
	}
	if updated.OAuthAccessToken != "new-access" {
		t.Fatalf("access token = %q, want %q", updated.OAuthAccessToken, "new-access")
	}
	if updated.OAuthRefreshToken != "new-refresh" {
		t.Fatalf("refresh token = %q, want %q", updated.OAuthRefreshToken, "new-refresh")
	}
}

func TestHandleTestProviderAccountReturnsAntigravityMetadata(t *testing.T) {
	resetProviderAccountsTestConfig(t)

	account := config.ProviderAccount{
		ID:                "antigravity-work",
		DisplayName:       "Antigravity Work",
		Type:              "antigravity",
		OAuthRefreshToken: "refresh-secret",
		OAuthAccessToken:  "access-secret",
		OAuthExpiry:       time.Now().Add(10 * time.Minute).Unix(),
		Email:             "dev@example.com",
		ProjectID:         "project-123",
	}
	config.Get().ProviderAccounts = append(config.Get().ProviderAccounts, account)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/provider-accounts/antigravity-work/test", nil)
	req.SetPathValue("id", "antigravity-work")
	rec := httptest.NewRecorder()

	var server Server
	server.handleTestProviderAccount(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if ok, _ := got["ok"].(bool); !ok {
		t.Fatalf("ok = %v, want true", got["ok"])
	}
	if status, _ := got["status"].(string); status != "connected" {
		t.Fatalf("status = %q, want connected", status)
	}
	if email, _ := got["email"].(string); email != "dev@example.com" {
		t.Fatalf("email = %q, want dev@example.com", email)
	}
	if projectID, _ := got["projectId"].(string); projectID != "project-123" {
		t.Fatalf("projectId = %q, want project-123", projectID)
	}
}
