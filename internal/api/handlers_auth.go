package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/auth"
)

type authProviderStatusResponse struct {
	Provider      string `json:"provider"`
	Authenticated bool   `json:"authenticated"`
	Source        string `json:"source,omitempty"`
	Message       string `json:"message"`
	EnterpriseURL string `json:"enterpriseUrl,omitempty"`
}

type authProviderMessageResponse struct {
	Message string `json:"message"`
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
