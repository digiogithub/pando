package llmproxy

import (
	"context"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// refreshAccountModels registers dynamic models for all configured, non-disabled
// provider accounts into the shared model registry. It mirrors
// app.RefreshDynamicModels so the proxy yields the same provider/account-prefixed
// model IDs as normal mode (e.g. "openai.gpt-4o", "work.gpt-4o"), keeping the
// /v1/models listing consistent with what resolveModel accepts.
//
// It is used as an on-demand fallback when the background refresh loop has not
// yet populated the registry for the configured providers.
func refreshAccountModels(ctx context.Context, accounts []config.ProviderAccount) {
	typeCount := make(map[models.ModelProvider]int)
	for _, acc := range accounts {
		typeCount[acc.Type]++
	}

	for _, acc := range accounts {
		apiKey := acc.APIKey
		var bearerToken string

		switch acc.Type {
		case models.ProviderCopilot:
			token, err := auth.LoadGitHubOAuthToken()
			if err != nil || token == "" {
				if session, err := auth.LoadCopilotSession(); err == nil && session != nil {
					token = session.AccessToken
				}
			}
			if token == "" {
				continue
			}
			bearerToken = token
			apiKey = ""
		case models.ProviderOllama, models.ProviderLlamaCpp:
			// No API key required for local providers.
		default:
			if apiKey == "" {
				continue
			}
		}

		params := models.AccountModelRefreshParams{
			AccountID:         acc.ID,
			ProviderType:      acc.Type,
			APIKey:            apiKey,
			BearerToken:       bearerToken,
			BaseURL:           acc.BaseURL,
			ExtraHeaders:      acc.ExtraHeaders,
			AllAccountsOfType: typeCount[acc.Type],
		}
		_ = models.RefreshProviderModelsForAccount(ctx, params)
	}
}
