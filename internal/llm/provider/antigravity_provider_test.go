package provider

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

func TestAntigravityProviderOptionsUseDedicatedGatewayTransport(t *testing.T) {
	account := config.ProviderAccount{
		ID:               "ag-work",
		Type:             models.ProviderAntigravity,
		OAuthAccessToken: "access-123",
		ProjectID:        "project-123",
	}
	model := models.AntigravityModels[models.AntigravityGemini30Pro]

	var opts providerClientOptions
	for _, apply := range antigravityProviderOptions(account, model, 4096, "system") {
		apply(&opts)
	}

	if opts.apiKey != "antigravity-oauth" {
		t.Fatalf("apiKey = %q, want antigravity-oauth", opts.apiKey)
	}
	if !opts.useOAuth {
		t.Fatal("expected useOAuth to be true")
	}
	if opts.oauthAccessToken != "access-123" {
		t.Fatalf("oauthAccessToken = %q, want access-123", opts.oauthAccessToken)
	}
	if len(opts.openaiOptions) != 2 {
		t.Fatalf("len(openaiOptions) = %d, want 2", len(opts.openaiOptions))
	}

	openaiOpts := openaiOptions{}
	for _, apply := range opts.openaiOptions {
		apply(&openaiOpts)
	}
	if openaiOpts.baseURL != antigravityGatewayBaseURL {
		t.Fatalf("baseURL = %q, want %q", openaiOpts.baseURL, antigravityGatewayBaseURL)
	}
	if got := openaiOpts.extraHeaders["Authorization"]; got != "Bearer access-123" {
		t.Fatalf("Authorization header = %q, want Bearer access-123", got)
	}
	if got := openaiOpts.extraHeaders["X-Antigravity-Project-ID"]; got != "project-123" {
		t.Fatalf("X-Antigravity-Project-ID = %q, want project-123", got)
	}
}

func TestPrepareAntigravityAccountUsesSchedulerAndRefreshesToken(t *testing.T) {
	setProviderConfigForTests(t,
		config.ProviderAccount{ID: "ag-1", Type: models.ProviderAntigravity, OAuthRefreshToken: "refresh-1", OAuthAccessToken: "access-1", OAuthExpiry: 4102444800},
	)
	ResetAntigravitySchedulerForTests()
	defer ResetAntigravitySchedulerForTests()

	model := models.AntigravityModels[models.AntigravityGemini30Pro]
	account := config.ProviderAccount{Type: models.ProviderAntigravity}

	prepared, ctx, err := prepareAntigravityAccount(account, model)
	if err != nil {
		t.Fatalf("prepareAntigravityAccount() error = %v", err)
	}
	if prepared.ID != "ag-1" {
		t.Fatalf("prepared account ID = %q, want ag-1", prepared.ID)
	}
	if ctx == nil {
		t.Fatal("expected antigravity request context")
	}
	if ctx.Account.ID != "ag-1" {
		t.Fatalf("context account ID = %q, want ag-1", ctx.Account.ID)
	}
	if ctx.Pool != AntigravityPoolGateway {
		t.Fatalf("context pool = %q, want %q", ctx.Pool, AntigravityPoolGateway)
	}
}
