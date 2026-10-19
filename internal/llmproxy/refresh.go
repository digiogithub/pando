package llmproxy

import (
	"context"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

const (
	// accountRefreshTimeout bounds one account's model fetch so an unreachable
	// endpoint cannot hold up /v1/models for the accounts that answer.
	accountRefreshTimeout = 5 * time.Second
	// accountRetryAfter is how long a failed or empty on-demand fetch is not
	// repeated for the same account.
	accountRetryAfter = 30 * time.Second
)

var (
	refreshAttemptsMu sync.Mutex
	refreshAttempts   = map[string]time.Time{}
)

// refreshAccountModels registers dynamic models for all configured, non-disabled
// provider accounts into the shared model registry. It mirrors
// app.RefreshDynamicModels so the proxy yields the same provider/account-prefixed
// model IDs as normal mode (e.g. "openai.gpt-4o", "work.gpt-4o"), keeping the
// /v1/models listing consistent with what resolveModel accepts.
func refreshAccountModels(ctx context.Context, accounts []config.ProviderAccount) {
	refreshAccounts(ctx, accounts, accounts)
}

// refreshMissingAccountModels fetches, concurrently and each with a short
// timeout, the models of the accounts that have none in the registry, at most
// once per accountRetryAfter per account. It is the lazy path of /v1/models:
// accounts that already have models are left alone, and an account that cannot
// be reached only costs its own timeout.
func refreshMissingAccountModels(ctx context.Context, accounts []config.ProviderAccount) {
	var missing []config.ProviderAccount
	now := time.Now()
	refreshAttemptsMu.Lock()
	for _, acc := range accounts {
		if accountHasModels(acc) {
			continue
		}
		key := acc.ID + "|" + acc.BaseURL
		if last, ok := refreshAttempts[key]; ok && now.Sub(last) < accountRetryAfter {
			continue
		}
		refreshAttempts[key] = now
		missing = append(missing, acc)
	}
	refreshAttemptsMu.Unlock()
	if len(missing) == 0 {
		return
	}
	refreshAccounts(ctx, accounts, missing)
}

// accountHasModels reports whether the registry holds a model for the account:
// one registered under its ID, or an account-less static model of its type.
func accountHasModels(acc config.ProviderAccount) bool {
	for _, m := range models.GetAllModels() {
		if m.Provider != acc.Type {
			continue
		}
		if m.AccountID == acc.ID || m.AccountID == "" {
			return true
		}
	}
	return false
}

// refreshAccounts fetches the models of the accounts in targets. all is the
// full set of active accounts, needed to decide the model id prefix (the
// account id is used only when several accounts share a type).
func refreshAccounts(ctx context.Context, all, targets []config.ProviderAccount) {
	typeCount := make(map[models.ModelProvider]int)
	for _, acc := range all {
		typeCount[acc.Type]++
	}

	var wg sync.WaitGroup
	for _, acc := range targets {
		apiKey := acc.APIKey
		var bearerToken string

		switch {
		case acc.Type == models.ProviderCopilot:
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
		case models.ProviderListsModelsWithoutAPIKey(acc.Type):
			// Local servers and OpenAI-compatible endpoints need no key.
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			accCtx, cancel := context.WithTimeout(ctx, accountRefreshTimeout)
			defer cancel()
			_ = models.RefreshProviderModelsForAccount(accCtx, params)
		}()
	}
	wg.Wait()
}
