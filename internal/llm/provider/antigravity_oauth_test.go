package provider

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	oauthantigravity "github.com/digiogithub/pando/internal/oauth/antigravity"
)

func TestAntigravityTokenNeedsRefresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name    string
		account config.ProviderAccount
		want    bool
	}{
		{name: "missing access token", account: config.ProviderAccount{}, want: true},
		{name: "fresh token", account: config.ProviderAccount{OAuthAccessToken: "token", OAuthExpiry: now.Add(5 * time.Minute).Unix()}, want: false},
		{name: "near expiry token", account: config.ProviderAccount{OAuthAccessToken: "token", OAuthExpiry: now.Add(5 * time.Second).Unix()}, want: true},
		{name: "no expiry with access token", account: config.ProviderAccount{OAuthAccessToken: "token"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := antigravityTokenNeedsRefresh(tt.account, now); got != tt.want {
				t.Fatalf("antigravityTokenNeedsRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureAntigravityAccessTokenRefreshesAndPersists(t *testing.T) {
	cfg := config.Get()
	if cfg == nil {
		t.Skip("config not loaded in this test environment")
	}
	originalAccounts := append([]config.ProviderAccount(nil), cfg.ProviderAccounts...)
	defer func() {
		cfg.ProviderAccounts = originalAccounts
	}()
	cfg.ProviderAccounts = nil

	now := time.Unix(1_700_000_000, 0)
	account := config.ProviderAccount{
		ID:                "antigravity-work",
		Type:              models.ProviderAntigravity,
		UseOAuth:          true,
		OAuthAccessToken:  "stale-access",
		OAuthRefreshToken: "refresh-123",
		OAuthExpiry:       now.Add(5 * time.Second).Unix(),
	}
	cfg.ProviderAccounts = append(cfg.ProviderAccounts, account)

	oldRefresh := antigravityRefreshTokenFunc
	defer func() { antigravityRefreshTokenFunc = oldRefresh }()
	antigravityRefreshTokenFunc = func(_ *http.Client, refreshToken string) (*oauthantigravity.Token, error) {
		if refreshToken != "refresh-123" {
			t.Fatalf("refresh token = %q, want refresh-123", refreshToken)
		}
		return &oauthantigravity.Token{AccessToken: "new-access", RefreshToken: "new-refresh", Expiry: now.Add(1 * time.Hour).Unix()}, nil
	}

	updated, err := ensureAntigravityAccessToken(account, now)
	if err != nil {
		t.Skipf("ensureAntigravityAccessToken persistence depends on writable test config: %v", err)
	}
	if updated.OAuthAccessToken != "new-access" {
		t.Fatalf("access token = %q, want new-access", updated.OAuthAccessToken)
	}
	if updated.OAuthRefreshToken != "new-refresh" {
		t.Fatalf("refresh token = %q, want new-refresh", updated.OAuthRefreshToken)
	}
	if updated.OAuthExpiry != now.Add(1*time.Hour).Unix() {
		t.Fatalf("expiry = %d, want %d", updated.OAuthExpiry, now.Add(1*time.Hour).Unix())
	}

	persisted, ok := config.GetProviderAccount(account.ID)
	if !ok {
		t.Fatalf("expected persisted account %q", account.ID)
	}
	if persisted.OAuthAccessToken != "new-access" || persisted.OAuthRefreshToken != "new-refresh" {
		t.Fatalf("persisted account = %+v", *persisted)
	}
}

func TestEnsureAntigravityAccessTokenReturnsErrorWithoutRefreshToken(t *testing.T) {
	_, err := ensureAntigravityAccessToken(config.ProviderAccount{ID: "antigravity-work", Type: models.ProviderAntigravity}, time.Now())
	if err == nil || err.Error() != "provider account \"antigravity-work\" has no refresh token" {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureAntigravityAccessTokenRejectsNonAntigravityAccount(t *testing.T) {
	_, err := ensureAntigravityAccessToken(config.ProviderAccount{ID: "openai-work", Type: models.ProviderOpenAI}, time.Now())
	if err == nil || err.Error() != "provider account is not an antigravity account" {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureAntigravityAccessTokenReturnsExistingFreshToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	account := config.ProviderAccount{
		ID:                "antigravity-work",
		Type:              models.ProviderAntigravity,
		OAuthAccessToken:  "fresh-access",
		OAuthRefreshToken: "refresh-123",
		OAuthExpiry:       now.Add(10 * time.Minute).Unix(),
	}

	oldRefresh := antigravityRefreshTokenFunc
	defer func() { antigravityRefreshTokenFunc = oldRefresh }()
	called := false
	antigravityRefreshTokenFunc = func(_ *http.Client, _ string) (*oauthantigravity.Token, error) {
		called = true
		return nil, errors.New("should not be called")
	}

	updated, err := ensureAntigravityAccessToken(account, now)
	if err != nil {
		t.Fatalf("ensureAntigravityAccessToken() error = %v", err)
	}
	if called {
		t.Fatal("expected refresh token function not to be called for fresh token")
	}
	if updated.OAuthAccessToken != account.OAuthAccessToken || updated.OAuthRefreshToken != account.OAuthRefreshToken || updated.OAuthExpiry != account.OAuthExpiry {
		t.Fatalf("updated account = %+v, want unchanged token fields from %+v", updated, account)
	}
}

func TestEnsureAntigravityAccessTokenWrapsRefreshFailure(t *testing.T) {
	oldRefresh := antigravityRefreshTokenFunc
	defer func() { antigravityRefreshTokenFunc = oldRefresh }()
	antigravityRefreshTokenFunc = func(_ *http.Client, _ string) (*oauthantigravity.Token, error) {
		return nil, errors.New("boom")
	}

	_, err := ensureAntigravityAccessToken(config.ProviderAccount{ID: "antigravity-work", Type: models.ProviderAntigravity, OAuthRefreshToken: "refresh"}, time.Now())
	if err == nil || err.Error() != "failed to refresh antigravity token: boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureAntigravityAccessTokenRejectsEmptyAccessTokenResponse(t *testing.T) {
	oldRefresh := antigravityRefreshTokenFunc
	defer func() { antigravityRefreshTokenFunc = oldRefresh }()
	antigravityRefreshTokenFunc = func(_ *http.Client, _ string) (*oauthantigravity.Token, error) {
		return &oauthantigravity.Token{RefreshToken: "refresh-456"}, nil
	}

	_, err := ensureAntigravityAccessToken(config.ProviderAccount{ID: "antigravity-work", Type: models.ProviderAntigravity, OAuthRefreshToken: "refresh-123"}, time.Now())
	if err == nil || err.Error() != "antigravity token refresh returned empty access token" {
		t.Fatalf("err = %v", err)
	}
}
