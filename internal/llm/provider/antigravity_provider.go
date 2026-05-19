package provider

import (
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

const (
	antigravityGatewayBaseURL = "https://antigravity.ai/api/v1"
)

func prepareAntigravityAccount(account config.ProviderAccount, model models.Model) (config.ProviderAccount, *antigravityRequestContext, error) {
	if account.Type != models.ProviderAntigravity {
		return config.ProviderAccount{}, nil, fmt.Errorf("provider account is not an antigravity account")
	}

	selectionAccount := account
	ctx := &antigravityRequestContext{
		Model: model,
	}
	if model.AccountID == "" {
		selection, err := defaultAntigravityScheduler.Select(model)
		if err != nil {
			return config.ProviderAccount{}, nil, err
		}
		selectionAccount = selection.Account
		ctx.Account = selection.Account
		ctx.Pool = selection.Pool
	} else {
		ctx.Account = account
		ctx.Pool = AntigravityPoolGateway
		if antigravityGeminiCLISupported(account) && isAntigravityGeminiModel(model) {
			ctx.Pool = AntigravityPoolGeminiCLI
		}
	}

	refreshed, err := ensureAntigravityAccessToken(selectionAccount, time.Now())
	if err != nil {
		return config.ProviderAccount{}, ctx, applyAntigravityFailure(selectionAccount, ctx.Pool, model, err)
	}
	ctx.Account = refreshed
	return refreshed, ctx, nil
}

func antigravityProviderOptions(account config.ProviderAccount, model models.Model, maxTokens int64, systemMessage string) []ProviderClientOption {
	extraHeaders := map[string]string{
		"Authorization": "Bearer " + account.OAuthAccessToken,
	}
	if account.ProjectID != "" {
		extraHeaders["X-Antigravity-Project-ID"] = account.ProjectID
	}

	return []ProviderClientOption{
		WithAPIKey("antigravity-oauth"),
		WithUseOAuth(true),
		WithOAuthAccount(account),
		WithModel(model),
		WithMaxTokens(maxTokens),
		WithSystemMessage(systemMessage),
		WithOpenAIOptions(
			WithOpenAIBaseURL(antigravityGatewayBaseURL),
			WithOpenAIExtraHeaders(extraHeaders),
		),
	}
}

func isAntigravityGeminiModel(model models.Model) bool {
	return antigravityModelFamily(model) != "" && len(antigravityPoolsForModel(model)) > 1
}
