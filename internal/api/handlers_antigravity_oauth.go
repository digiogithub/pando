package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	oauthantigravity "github.com/digiogithub/pando/internal/oauth/antigravity"
)

type antigravityOAuthStartRequest struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	RedirectURI  string `json:"redirectUri"`
	CallbackPath string `json:"callbackPath"`
}

type antigravityOAuthStartResponse struct {
	AccountID    string `json:"accountId"`
	AuthURL      string `json:"authUrl"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirectUri"`
	CallbackPath string `json:"callbackPath,omitempty"`
}

type antigravityOAuthCallbackRequest struct {
	AccountID string `json:"accountId"`
	Code      string `json:"code"`
	State     string `json:"state"`
}

type antigravityOAuthRefreshRequest struct {
	AccountID string `json:"accountId"`
	Force     bool   `json:"force"`
}

type antigravityOAuthVerifyRequest struct {
	AccountID string `json:"accountId"`
}

type antigravityOAuthResponse struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName,omitempty"`
	Email        string `json:"email,omitempty"`
	ProjectID    string `json:"projectId,omitempty"`
	RedirectURI  string `json:"redirectUri,omitempty"`
	Connected    bool   `json:"connected"`
	TokenExpiry  int64  `json:"tokenExpiry,omitempty"`
	NeedsRefresh bool   `json:"needsRefresh,omitempty"`
}

func (s *Server) handleAntigravityOAuthStart(w http.ResponseWriter, r *http.Request) {
	var req antigravityOAuthStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.AccountID) == "" {
		writeError(w, http.StatusBadRequest, "accountId is required")
		return
	}
	if !slugRe.MatchString(req.AccountID) {
		writeError(w, http.StatusBadRequest, "accountId must match ^[a-z0-9-]+$")
		return
	}

	redirectURI := strings.TrimSpace(req.RedirectURI)
	if redirectURI == "" {
		redirectURI = s.defaultAntigravityRedirectURI(req.CallbackPath)
	}
	if _, err := parseAbsoluteURL(redirectURI); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	session, err := oauthantigravity.NewSession(redirectURI)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create oauth session: "+err.Error())
		return
	}
	authURL, err := session.AuthURL()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build auth URL: "+err.Error())
		return
	}

	account := config.ProviderAccount{
		ID:                req.AccountID,
		DisplayName:       strings.TrimSpace(req.DisplayName),
		Type:              models.ProviderAntigravity,
		UseOAuth:          true,
		OAuthState:        session.State,
		OAuthCodeVerifier: session.CodeVerifier,
		OAuthRedirectURI:  redirectURI,
	}
	if account.DisplayName == "" {
		account.DisplayName = req.AccountID
	}

	if existing, ok := config.GetProviderAccount(req.AccountID); ok {
		merged := *existing
		merged.DisplayName = account.DisplayName
		merged.Type = models.ProviderAntigravity
		merged.UseOAuth = true
		merged.OAuthState = account.OAuthState
		merged.OAuthCodeVerifier = account.OAuthCodeVerifier
		merged.OAuthRedirectURI = account.OAuthRedirectURI
		if err := config.UpdateProviderAccount(req.AccountID, merged); err != nil {
			writeError(w, http.StatusBadRequest, "failed to update provider account: "+err.Error())
			return
		}
	} else {
		if err := config.AddProviderAccount(account); err != nil {
			writeError(w, http.StatusBadRequest, "failed to create provider account: "+err.Error())
			return
		}
	}

	publishProviderAccountChanged()
	writeJSON(w, http.StatusOK, antigravityOAuthStartResponse{
		AccountID:    req.AccountID,
		AuthURL:      authURL,
		State:        session.State,
		RedirectURI:  redirectURI,
		CallbackPath: strings.TrimSpace(req.CallbackPath),
	})
}

func (s *Server) handleAntigravityOAuthCallback(w http.ResponseWriter, r *http.Request) {
	var req antigravityOAuthCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	account, ok := config.GetProviderAccount(strings.TrimSpace(req.AccountID))
	if !ok {
		writeError(w, http.StatusNotFound, "provider account not found: "+strings.TrimSpace(req.AccountID))
		return
	}
	if account.Type != models.ProviderAntigravity {
		writeError(w, http.StatusBadRequest, "provider account is not an antigravity account")
		return
	}

	session, err := antigravitySessionFromAccount(*account)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := session.ValidateCallback(strings.TrimSpace(req.State)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := oauthantigravity.ExchangeCode(nil, req.Code, session.CodeVerifier, session.RedirectURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to exchange authorization code: "+err.Error())
		return
	}
	if strings.TrimSpace(token.RefreshToken) == "" && strings.TrimSpace(account.OAuthRefreshToken) != "" {
		token.RefreshToken = account.OAuthRefreshToken
	}

	updated, err := s.hydrateAntigravityAccount(*account, *token)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := config.UpdateProviderAccount(account.ID, updated); err != nil {
		writeError(w, http.StatusBadRequest, "failed to persist provider account: "+err.Error())
		return
	}

	publishProviderAccountChanged()
	writeJSON(w, http.StatusOK, antigravityOAuthResponseFromAccount(updated))
}

func (s *Server) handleAntigravityOAuthRefresh(w http.ResponseWriter, r *http.Request) {
	var req antigravityOAuthRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := s.refreshAntigravityAccount(strings.TrimSpace(req.AccountID), req.Force)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	publishProviderAccountChanged()
	writeJSON(w, http.StatusOK, antigravityOAuthResponseFromAccount(updated))
}

func (s *Server) handleAntigravityOAuthVerify(w http.ResponseWriter, r *http.Request) {
	var req antigravityOAuthVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	accountID := strings.TrimSpace(req.AccountID)
	account, ok := config.GetProviderAccount(accountID)
	if !ok {
		writeError(w, http.StatusNotFound, "provider account not found: "+accountID)
		return
	}
	if account.Type != models.ProviderAntigravity {
		writeError(w, http.StatusBadRequest, "provider account is not an antigravity account")
		return
	}
	if strings.TrimSpace(account.OAuthAccessToken) == "" && strings.TrimSpace(account.OAuthRefreshToken) == "" {
		writeJSON(w, http.StatusOK, antigravityOAuthResponse{AccountID: account.ID, DisplayName: account.DisplayName, Connected: false})
		return
	}

	refreshed, err := s.refreshAntigravityAccount(account.ID, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	publishProviderAccountChanged()
	writeJSON(w, http.StatusOK, antigravityOAuthResponseFromAccount(refreshed))
}

func (s *Server) refreshAntigravityAccount(accountID string, force bool) (config.ProviderAccount, error) {
	account, ok := config.GetProviderAccount(accountID)
	if !ok {
		return config.ProviderAccount{}, fmt.Errorf("provider account not found: %s", accountID)
	}
	if account.Type != models.ProviderAntigravity {
		return config.ProviderAccount{}, fmt.Errorf("provider account is not an antigravity account")
	}
	if strings.TrimSpace(account.OAuthRefreshToken) == "" {
		return config.ProviderAccount{}, fmt.Errorf("provider account %q has no refresh token", account.ID)
	}
	if !force && !provider.AntigravityTokenNeedsRefresh(*account) {
		return *account, nil
	}

	token, err := oauthantigravity.RefreshToken(nil, account.OAuthRefreshToken)
	if err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to refresh antigravity token: %w", err)
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		token.RefreshToken = account.OAuthRefreshToken
	}

	updated, err := s.hydrateAntigravityAccount(*account, *token)
	if err != nil {
		return config.ProviderAccount{}, err
	}
	if err := config.UpdateProviderAccount(account.ID, updated); err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to persist provider account: %w", err)
	}
	return updated, nil
}

func (s *Server) hydrateAntigravityAccount(account config.ProviderAccount, token oauthantigravity.Token) (config.ProviderAccount, error) {
	userInfo, err := oauthantigravity.FetchUserInfo(nil, token.AccessToken)
	if err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to fetch google account email: %w", err)
	}

	projectID := strings.TrimSpace(token.ProjectID)
	if projectID == "" {
		projectID, err = oauthantigravity.FetchProjectID(nil, token.AccessToken)
		if err != nil {
			// Fallback to an empty project ID if resolution fails, as some accounts might not have one
			// but we still want to allow login. Or we can return the error.
			// Let's log it but continue, or use a fallback.
		}
	}

	updated := account
	updated.Type = models.ProviderAntigravity
	updated.UseOAuth = true
	updated.OAuthAccessToken = token.AccessToken
	updated.OAuthRefreshToken = token.RefreshToken
	updated.OAuthExpiry = token.Expiry
	updated.Email = userInfo.Email
	updated.ProjectID = projectID
	updated.OAuthState = ""
	updated.OAuthCodeVerifier = ""
	updated.OAuthRedirectURI = account.OAuthRedirectURI
	updated.ReauthRequired = false
	return updated, nil
}

func antigravityOAuthResponseFromAccount(account config.ProviderAccount) antigravityOAuthResponse {
	return antigravityOAuthResponse{
		AccountID:    account.ID,
		DisplayName:  account.DisplayName,
		Email:        account.Email,
		ProjectID:    account.ProjectID,
		RedirectURI:  account.OAuthRedirectURI,
		Connected:    strings.TrimSpace(account.OAuthAccessToken) != "" || strings.TrimSpace(account.OAuthRefreshToken) != "",
		TokenExpiry:  account.OAuthExpiry,
		NeedsRefresh: provider.AntigravityTokenNeedsRefresh(account),
	}
}

func antigravitySessionFromAccount(account config.ProviderAccount) (oauthantigravity.Session, error) {
	state := strings.TrimSpace(account.OAuthState)
	verifier := strings.TrimSpace(account.OAuthCodeVerifier)
	redirectURI := strings.TrimSpace(account.OAuthRedirectURI)
	if state == "" || verifier == "" || redirectURI == "" {
		return oauthantigravity.Session{}, fmt.Errorf("provider account %q is missing oauth session data", account.ID)
	}
	return oauthantigravity.Session{State: state, CodeVerifier: verifier, RedirectURI: redirectURI}, nil
}

func parseAbsoluteURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("redirectUri must be an absolute URL")
	}
	return parsed.String(), nil
}

// handleAntigravityOAuthRedirect is the GET handler for the OAuth redirect from Google.
// Google sends: GET /...?code=...&state=...
// It finds the account by state, exchanges the code, and redirects to the settings page.
func (s *Server) handleAntigravityOAuthRedirect(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape(errParam), http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape("missing code or state"), http.StatusFound)
		return
	}

	// Find the provider account that owns this OAuth state.
	account := findAccountByOAuthState(state)
	if account == nil {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape("invalid OAuth state"), http.StatusFound)
		return
	}

	session, err := antigravitySessionFromAccount(*account)
	if err != nil {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	if err := session.ValidateCallback(state); err != nil {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	token, err := oauthantigravity.ExchangeCode(nil, code, session.CodeVerifier, session.RedirectURI)
	if err != nil {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape("failed to exchange code: "+err.Error()), http.StatusFound)
		return
	}
	if strings.TrimSpace(token.RefreshToken) == "" && strings.TrimSpace(account.OAuthRefreshToken) != "" {
		token.RefreshToken = account.OAuthRefreshToken
	}

	updated, err := s.hydrateAntigravityAccount(*account, *token)
	if err != nil {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	if err := config.UpdateProviderAccount(account.ID, updated); err != nil {
		http.Redirect(w, r, "/settings?authError="+url.QueryEscape("failed to save account: "+err.Error()), http.StatusFound)
		return
	}

	publishProviderAccountChanged()
	http.Redirect(w, r, "/settings?authSuccess=antigravity&account="+url.QueryEscape(account.ID), http.StatusFound)
}

// findAccountByOAuthState returns the provider account whose OAuthState matches state.
func findAccountByOAuthState(state string) *config.ProviderAccount {
	for _, a := range config.GetProviderAccounts() {
		if strings.TrimSpace(a.OAuthState) == state {
			copy := a
			return &copy
		}
	}
	return nil
}

func (s *Server) defaultAntigravityRedirectURI(callbackPath string) string {
	scheme := "http"
	if s.IsTLS() {
		scheme = "https"
	}
	host := s.config.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	port := s.config.Port
	path := strings.TrimSpace(callbackPath)
	if path == "" {
		// Default to the GET redirect handler so Google's redirect works correctly.
		path = "/api/v1/config/provider-accounts/antigravity/oauth-redirect"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)
}
