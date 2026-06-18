package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/stats"
)

type authProviderStatusResponse struct {
	Provider         string `json:"provider"`
	Authenticated    bool   `json:"authenticated"`
	Source           string `json:"source,omitempty"`
	Message          string `json:"message"`
	DisplayName      string `json:"displayName,omitempty"`
	Email            string `json:"email,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
	EnterpriseURL    string `json:"enterpriseUrl,omitempty"`
}

type authProviderMessageResponse struct {
	Message string `json:"message"`
}

type anthropicLoginStartResponse struct {
	ManualURL string `json:"manualUrl"`
	AutoURL   string `json:"autoUrl"`
	Message   string `json:"message"`
}

type anthropicLoginCompleteRequest struct {
	Input string `json:"input"`
}

type anthropicStatsResponse struct {
	Content string `json:"content"`
}

type copilotLoginRequest struct {
	EnterpriseURL string `json:"enterpriseUrl,omitempty"`
}

type copilotLoginStartResponse struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
	ExpiresIn       int    `json:"expiresIn,omitempty"`
	Interval        int    `json:"interval,omitempty"`
	EnterpriseURL   string `json:"enterpriseUrl,omitempty"`
	Message         string `json:"message"`
}

func (s *Server) handleAuthProviderStatus(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(strings.ToLower(r.PathValue("provider")))
	switch provider {
	case "anthropic":
		status, err := auth.GetClaudeAuthStatus()
		if err != nil && status == nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		message := "Claude.ai authentication not configured"
		if status != nil && status.Authenticated {
			message = "Claude.ai authenticated"
		}
		if err != nil {
			message = err.Error()
		}
		writeJSON(w, http.StatusOK, authProviderStatusResponse{
			Provider:         provider,
			Authenticated:    status != nil && status.Authenticated,
			Source:           status.Source,
			Message:          message,
			DisplayName:      status.DisplayName,
			Email:            status.Email,
			SubscriptionType: status.SubscriptionType,
		})
	case "copilot":
		status := auth.GetCopilotAuthStatus()
		writeJSON(w, http.StatusOK, authProviderStatusResponse{
			Provider:      provider,
			Authenticated: status.Authenticated,
			Source:        status.Source,
			Message:       status.Message,
			EnterpriseURL: status.EnterpriseURL,
		})
	default:
		writeError(w, http.StatusNotFound, "unsupported auth provider: "+provider)
	}
}

func (s *Server) handleAnthropicLoginStart(w http.ResponseWriter, r *http.Request) {
	session, err := auth.ClaudeLoginStart()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start Claude login: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, anthropicLoginStartResponse{
		ManualURL: session.ManualURL,
		AutoURL:   session.AutoURL,
		Message:   "Claude login started. Your browser should open automatically; use complete login if you need to paste the code manually.",
	})
}

func (s *Server) handleAnthropicLoginComplete(w http.ResponseWriter, r *http.Request) {
	var req anthropicLoginCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input := strings.TrimSpace(req.Input)
	if input == "" {
		writeError(w, http.StatusBadRequest, "input is required")
		return
	}

	session, err := auth.ClaudeLoginStart()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "start Claude login: "+err.Error())
		return
	}
	defer session.Cancel()

	code := session.ExtractCodeFromInput(input)
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid Claude authorization code or callback URL")
		return
	}

	redirectURI := auth.ClaudeManualRedirectURL
	if strings.Contains(input, "localhost") {
		redirectURI = session.AutoRedirectURI
	}

	creds, displayName, err := auth.ClaudeLoginFinish(session, code, redirectURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, "complete Claude login: "+err.Error())
		return
	}
	if err := auth.SaveClaudeCredentials(creds); err != nil {
		writeError(w, http.StatusInternalServerError, "save Claude credentials: "+err.Error())
		return
	}

	status, _ := auth.GetClaudeAuthStatus()
	writeJSON(w, http.StatusOK, authProviderStatusResponse{
		Provider:         "anthropic",
		Authenticated:    true,
		Source:           status.Source,
		Message:          "Claude.ai authenticated",
		DisplayName:      firstNonEmpty(displayName, status.DisplayName),
		Email:            status.Email,
		SubscriptionType: status.SubscriptionType,
	})
}

func (s *Server) handleAnthropicLogout(w http.ResponseWriter, r *http.Request) {
	if err := auth.ClaudeLogout(); err != nil {
		writeError(w, http.StatusInternalServerError, "logout Claude: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, authProviderMessageResponse{Message: "Claude.ai credentials removed"})
}

func (s *Server) handleAnthropicStats(w http.ResponseWriter, r *http.Request) {
	cache, err := stats.LoadBestAvailableStats()
	if err != nil || cache == nil {
		writeJSON(w, http.StatusOK, anthropicStatsResponse{Content: "No usage statistics available.\n\nUse Claude Code or Pando with a Claude account to track usage."})
		return
	}
	writeJSON(w, http.StatusOK, anthropicStatsResponse{Content: stats.FormatStats(cache)})
}

func (s *Server) handleCopilotLoginStart(w http.ResponseWriter, r *http.Request) {
	var req copilotLoginRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	deviceCode, err := auth.StartCopilotDeviceFlow(ctx, req.EnterpriseURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "start Copilot login: "+err.Error())
		return
	}
	_ = auth.OpenBrowser(deviceCode.VerificationURI)

	go func(deviceCode *auth.CopilotDeviceCode, enterpriseURL string) {
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer bgCancel()
		if _, err := auth.CompleteCopilotDeviceFlow(bgCtx, enterpriseURL, deviceCode); err != nil {
			return
		}
		// The Copilot OAuth token is now stored. Fetch the account's models so
		// they become selectable in the current process without a restart;
		// otherwise the model switcher and agent validation reject every Copilot
		// model until the next startup/24h refresh.
		refreshDynamicModelsAfterAccountChange()
		publishProviderAccountChanged()
	}(deviceCode, req.EnterpriseURL)

	writeJSON(w, http.StatusOK, copilotLoginStartResponse{
		VerificationURI: deviceCode.VerificationURI,
		UserCode:        deviceCode.UserCode,
		ExpiresIn:       deviceCode.ExpiresIn,
		Interval:        deviceCode.Interval,
		EnterpriseURL:   req.EnterpriseURL,
		Message:         "Copilot device login started. Open the verification URL and enter the code, then check status in a few seconds.",
	})
}

func (s *Server) handleCopilotLogout(w http.ResponseWriter, r *http.Request) {
	if err := auth.DeleteCopilotSession(); err != nil {
		writeError(w, http.StatusInternalServerError, "logout Copilot: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, authProviderMessageResponse{Message: "GitHub Copilot credentials removed"})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
