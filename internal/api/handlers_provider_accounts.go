package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
)

var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// ProviderTypeInfo describes a supported provider type and its requirements.
type ProviderTypeInfo struct {
	Type                 string `json:"type"`
	DisplayName          string `json:"displayName"`
	RequiresAPIKey       bool   `json:"requiresAPIKey"`
	RequiresBaseURL      bool   `json:"requiresBaseUrl"`
	SupportsOAuth        bool   `json:"supportsOAuth"`
	SupportsExtraHeaders bool   `json:"supportsExtraHeaders"`
}

var providerTypes = []ProviderTypeInfo{
	{Type: "anthropic", DisplayName: "Anthropic", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: true, SupportsExtraHeaders: true},
	{Type: "antigravity", DisplayName: "Antigravity", RequiresAPIKey: false, RequiresBaseURL: false, SupportsOAuth: true, SupportsExtraHeaders: false},
	{Type: "openai", DisplayName: "OpenAI", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: false, SupportsExtraHeaders: true},
	{Type: "openai-compatible", DisplayName: "OpenAI Compatible", RequiresAPIKey: true, RequiresBaseURL: true, SupportsOAuth: false, SupportsExtraHeaders: true},
	{Type: "ollama", DisplayName: "Ollama", RequiresAPIKey: false, RequiresBaseURL: true, SupportsOAuth: false, SupportsExtraHeaders: false},
	{Type: "copilot", DisplayName: "GitHub Copilot", RequiresAPIKey: false, RequiresBaseURL: false, SupportsOAuth: true, SupportsExtraHeaders: false},
	{Type: "gemini", DisplayName: "Google Gemini", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: false, SupportsExtraHeaders: false},
	{Type: "groq", DisplayName: "Groq", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: false, SupportsExtraHeaders: false},
	{Type: "openrouter", DisplayName: "OpenRouter", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: false, SupportsExtraHeaders: true},
	{Type: "xai", DisplayName: "xAI", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: false, SupportsExtraHeaders: false},
	{Type: "azure", DisplayName: "Azure OpenAI", RequiresAPIKey: true, RequiresBaseURL: true, SupportsOAuth: false, SupportsExtraHeaders: true},
	{Type: "bedrock", DisplayName: "AWS Bedrock", RequiresAPIKey: true, RequiresBaseURL: false, SupportsOAuth: false, SupportsExtraHeaders: false},
	{Type: "vertexai", DisplayName: "Google Vertex AI", RequiresAPIKey: false, RequiresBaseURL: false, SupportsOAuth: true, SupportsExtraHeaders: false},
}

func maskProviderAccountAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "***"
	}
	return "***" + key[len(key)-4:]
}

func providerAccountToResponse(a config.ProviderAccount, mask bool) config.ProviderAccount {
	out := a
	if mask && out.APIKey != "" {
		out.APIKey = maskProviderAccountAPIKey(out.APIKey)
	}
	out.OAuthRefreshToken = ""
	out.OAuthAccessToken = ""
	return out
}

func publishProviderAccountChanged() {
	config.Bus.Publish(config.ConfigChangeEvent{
		Section:   "provider-accounts",
		Timestamp: time.Now(),
	})
}

// refreshDynamicModelsAfterAccountChange re-fetches and registers the models for
// every configured provider account so a newly added/updated account's models
// become selectable immediately. Without this, dynamic models (e.g. Copilot's
// gpt-5.x) are only registered at startup or on the 24h refresh, so right after
// adding an account the model switcher and agent validation reject every model
// from it ("model not supported"), and the only workaround was to restart the
// app (which the TUI implicitly did). Runs with a bounded timeout that is
// independent of the HTTP request lifetime so it is not cancelled when the
// handler returns its response.
func refreshDynamicModelsAfterAccountChange() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app.RefreshDynamicModels(ctx)
}

func (s *Server) handleListProviderAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := config.GetProviderAccounts()
	result := make([]config.ProviderAccount, 0, len(accounts))
	for _, a := range accounts {
		result = append(result, providerAccountToResponse(a, true))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"providerAccounts": result})
}

func (s *Server) handleCreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	var account config.ProviderAccount
	if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(account.ID) == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if !slugRe.MatchString(account.ID) {
		writeError(w, http.StatusBadRequest, "id must match ^[a-z0-9-]+$")
		return
	}
	if strings.TrimSpace(account.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	if account.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}

	if err := config.AddProviderAccount(account); err != nil {
		writeError(w, http.StatusBadRequest, "failed to create provider account: "+err.Error())
		return
	}

	refreshDynamicModelsAfterAccountChange()
	publishProviderAccountChanged()

	created, ok := config.GetProviderAccount(account.ID)
	if !ok {
		writeError(w, http.StatusInternalServerError, "account created but could not be retrieved")
		return
	}

	writeJSON(w, http.StatusCreated, providerAccountToResponse(*created, true))
}

func (s *Server) handleGetProviderAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, ok := config.GetProviderAccount(id)
	if !ok {
		writeError(w, http.StatusNotFound, "provider account not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, providerAccountToResponse(*account, true))
}

func (s *Server) handleUpdateProviderAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	existing, ok := config.GetProviderAccount(id)
	if !ok {
		writeError(w, http.StatusNotFound, "provider account not found: "+id)
		return
	}

	var updated config.ProviderAccount
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Do not overwrite the existing API key when the incoming value is masked.
	if updated.APIKey == "" || strings.HasPrefix(updated.APIKey, "***") {
		updated.APIKey = existing.APIKey
	}

	updated.ID = id

	if err := config.UpdateProviderAccount(id, updated); err != nil {
		writeError(w, http.StatusBadRequest, "failed to update provider account: "+err.Error())
		return
	}

	refreshDynamicModelsAfterAccountChange()
	publishProviderAccountChanged()

	result, ok := config.GetProviderAccount(id)
	if !ok {
		writeError(w, http.StatusInternalServerError, "account updated but could not be retrieved")
		return
	}

	writeJSON(w, http.StatusOK, providerAccountToResponse(*result, true))
}

func (s *Server) handleDeleteProviderAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := config.DeleteProviderAccount(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	publishProviderAccountChanged()

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleTestProviderAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	account, ok := config.GetProviderAccount(id)
	if !ok {
		writeError(w, http.StatusNotFound, "provider account not found: "+id)
		return
	}

	if account.Disabled {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":    false,
			"error": "account is disabled",
		})
		return
	}

	if account.Type == models.ProviderAntigravity {
		result, err := s.verifyAntigravityAccount(*account)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":        false,
				"error":     err.Error(),
				"accountId": account.ID,
				"provider":  account.Type,
			})
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"modelCount": 0,
	})
}

func (s *Server) verifyAntigravityAccount(account config.ProviderAccount) (map[string]interface{}, error) {
	response := antigravityOAuthResponseFromAccount(account)
	connected := response.Connected
	status := antigravityAccountStatus(account)

	result := map[string]interface{}{
		"ok":           connected && !response.NeedsRefresh,
		"provider":     account.Type,
		"accountId":    account.ID,
		"connected":    connected,
		"displayName":  account.DisplayName,
		"email":        account.Email,
		"projectId":    account.ProjectID,
		"status":       status,
		"tokenExpiry":  account.OAuthExpiry,
		"needsRefresh": response.NeedsRefresh,
		"modelCount":   0,
	}

	if !connected {
		result["error"] = "account is not connected"
		return result, nil
	}
	if !response.NeedsRefresh {
		return result, nil
	}

	refreshed, err := s.refreshAntigravityAccount(account.ID, false)
	if err != nil {
		result["error"] = fmt.Sprintf("account requires reauthentication: %v", err)
		return result, nil
	}

	response = antigravityOAuthResponseFromAccount(refreshed)
	result["ok"] = true
	result["email"] = refreshed.Email
	result["projectId"] = refreshed.ProjectID
	result["tokenExpiry"] = refreshed.OAuthExpiry
	result["needsRefresh"] = response.NeedsRefresh
	result["status"] = antigravityAccountStatus(refreshed)
	return result, nil
}

func antigravityAccountStatus(account config.ProviderAccount) string {
	if account.Disabled {
		return "disabled"
	}
	if strings.TrimSpace(account.OAuthAccessToken) == "" && strings.TrimSpace(account.OAuthRefreshToken) == "" {
		return "pending"
	}
	if provider.AntigravityTokenNeedsRefresh(account) {
		return "needs_refresh"
	}
	return "connected"
}

func (s *Server) handleListProviderTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"providerTypes": providerTypes})
}
