package auth

import (
	"context"
	"testing"
	"time"
)

func TestParseCopilotAPIToken(t *testing.T) {
	body := []byte(`{
		"token": "tid=abc;exp=1754000000",
		"expires_at": 1754000000,
		"sku": "copilot_for_business_seat_quota",
		"organization_list": ["ed89c5dc51b7d442caede4f28123083c"],
		"endpoints": {"api": "https://api.business.githubcopilot.com/"}
	}`)

	token, err := parseCopilotAPIToken(body)
	if err != nil {
		t.Fatalf("parseCopilotAPIToken: %v", err)
	}
	if token.Token != "tid=abc;exp=1754000000" {
		t.Errorf("token = %q", token.Token)
	}
	if token.APIEndpoint != "https://api.business.githubcopilot.com" {
		t.Errorf("APIEndpoint = %q, want trailing slash stripped", token.APIEndpoint)
	}
	if len(token.Organizations) != 1 {
		t.Errorf("Organizations = %v", token.Organizations)
	}
	if token.SKU != "copilot_for_business_seat_quota" {
		t.Errorf("SKU = %q", token.SKU)
	}
}

func TestParseCopilotAPITokenRejectsEmptyToken(t *testing.T) {
	if _, err := parseCopilotAPIToken([]byte(`{"expires_at": 1}`)); err == nil {
		t.Fatal("expected error for response without token")
	}
}

func TestIsCopilotAPIToken(t *testing.T) {
	if !IsCopilotAPIToken("tid=abc;exp=123;sku=free") {
		t.Error("exchanged copilot token not detected")
	}
	if IsCopilotAPIToken("gho_deadbeef") {
		t.Error("oauth token misdetected as copilot api token")
	}
}

func TestCopilotAPITokenExpired(t *testing.T) {
	var nilToken *CopilotAPIToken
	if !nilToken.expired() {
		t.Error("nil token must count as expired")
	}
	if !(&CopilotAPIToken{Token: "tid=x"}).expired() {
		t.Error("token without expiry must count as expired")
	}
	if !(&CopilotAPIToken{Token: "tid=x", ExpiresAt: time.Now().Add(30 * time.Second).Unix()}).expired() {
		t.Error("token inside the renew margin must count as expired")
	}
	if (&CopilotAPIToken{Token: "tid=x", ExpiresAt: time.Now().Add(30 * time.Minute).Unix()}).expired() {
		t.Error("token well before expiry must not count as expired")
	}
}

func TestCopilotTokenExchangeURL(t *testing.T) {
	if got := copilotTokenExchangeURL(""); got != "https://api.github.com/copilot_internal/v2/token" {
		t.Errorf("github.com url = %q", got)
	}
	if got := copilotTokenExchangeURL("https://ghe.example.com/"); got != "https://ghe.example.com/api/v3/copilot_internal/v2/token" {
		t.Errorf("enterprise url = %q", got)
	}
}

func TestResolveCopilotAPIAccessFallsBack(t *testing.T) {
	// PANDO_COPILOT_TOKEN_EXCHANGE=0 forces the legacy path, which is also what
	// happens whenever the exchange request fails: the raw token and the default
	// host must be returned unchanged.
	t.Setenv("PANDO_COPILOT_TOKEN_EXCHANGE", "0")

	token, baseURL := ResolveCopilotAPIAccess(context.Background(), "gho_test", "", "")
	if token != "gho_test" {
		t.Errorf("token = %q, want the raw oauth token", token)
	}
	if baseURL != "https://api.githubcopilot.com" {
		t.Errorf("baseURL = %q", baseURL)
	}
}

func TestResolveCopilotAPIAccessKeepsConfiguredBaseURL(t *testing.T) {
	t.Setenv("PANDO_COPILOT_TOKEN_EXCHANGE", "0")

	_, baseURL := ResolveCopilotAPIAccess(context.Background(), "gho_test", "", "https://proxy.internal/copilot/")
	if baseURL != "https://proxy.internal/copilot" {
		t.Errorf("baseURL = %q, want the configured host to win", baseURL)
	}
}

func TestResolveCopilotAPIAccessSkipsAlreadyExchangedToken(t *testing.T) {
	token, baseURL := ResolveCopilotAPIAccess(context.Background(), "tid=abc;exp=1", "", "")
	if token != "tid=abc;exp=1" {
		t.Errorf("token = %q, want it returned untouched", token)
	}
	if baseURL != "https://api.githubcopilot.com" {
		t.Errorf("baseURL = %q", baseURL)
	}
}
