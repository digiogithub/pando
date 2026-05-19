package provider

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	oauthantigravity "github.com/digiogithub/pando/internal/oauth/antigravity"
)

const antigravityTokenRefreshSkew = 30 * time.Second

var antigravityRefreshTokenFunc = oauthantigravity.RefreshToken

func antigravityTokenNeedsRefresh(account config.ProviderAccount, now time.Time) bool {
	if strings.TrimSpace(account.OAuthAccessToken) == "" {
		return true
	}
	if account.OAuthExpiry == 0 {
		return false
	}
	return now.Add(antigravityTokenRefreshSkew).Unix() >= account.OAuthExpiry
}

func AntigravityTokenNeedsRefresh(account config.ProviderAccount) bool {
	return antigravityTokenNeedsRefresh(account, time.Now())
}

func ensureAntigravityAccessToken(account config.ProviderAccount, now time.Time) (config.ProviderAccount, error) {
	if account.Type != models.ProviderAntigravity {
		return config.ProviderAccount{}, fmt.Errorf("provider account is not an antigravity account")
	}
	if !antigravityTokenNeedsRefresh(account, now) {
		return account, nil
	}
	if strings.TrimSpace(account.OAuthRefreshToken) == "" {
		return config.ProviderAccount{}, fmt.Errorf("provider account %q has no refresh token", account.ID)
	}

	token, err := antigravityRefreshTokenFunc(nil, account.OAuthRefreshToken)
	if err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to refresh antigravity token: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return config.ProviderAccount{}, fmt.Errorf("antigravity token refresh returned empty access token")
	}

	updated := account
	updated.OAuthAccessToken = token.AccessToken
	if strings.TrimSpace(token.RefreshToken) != "" {
		updated.OAuthRefreshToken = token.RefreshToken
	}
	if token.Expiry != 0 {
		updated.OAuthExpiry = token.Expiry
	}
	updated.ReauthRequired = false

	if err := config.UpdateProviderAccount(account.ID, updated); err != nil {
		return config.ProviderAccount{}, fmt.Errorf("failed to persist provider account: %w", err)
	}

	return updated, nil
}

type antigravityRefreshTokenFn func(client *http.Client, refreshToken string) (*oauthantigravity.Token, error)
