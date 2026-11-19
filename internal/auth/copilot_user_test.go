package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func stubCopilotUser(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	old := copilotUserURLOverride
	copilotUserURLOverride = server.URL
	t.Cleanup(func() { copilotUserURLOverride = old })

	copilotEndpointCacheMu.Lock()
	copilotEndpointCache = map[string]copilotEndpointEntry{}
	copilotEndpointCacheMu.Unlock()
}

func TestParseCopilotUser(t *testing.T) {
	user, err := parseCopilotUser([]byte(`{
		"chat_enabled": true,
		"copilot_plan": "business",
		"endpoints": {"api": "https://api.business.githubcopilot.com/"}
	}`))
	if err != nil {
		t.Fatalf("parseCopilotUser: %v", err)
	}
	if user.ChatEnabled == nil || !*user.ChatEnabled {
		t.Errorf("ChatEnabled = %v, want true", user.ChatEnabled)
	}
	if user.APIEndpoint != "https://api.business.githubcopilot.com" {
		t.Errorf("APIEndpoint = %q, want trailing slash stripped", user.APIEndpoint)
	}
	if user.Plan != "business" {
		t.Errorf("Plan = %q", user.Plan)
	}
}

func TestCopilotEntitlementError(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name   string
		user   *CopilotUser
		denied bool
	}{
		{"nil user", nil, false},
		{"field missing", &CopilotUser{}, false},
		{"chat enabled", &CopilotUser{ChatEnabled: &yes}, false},
		{"no access", &CopilotUser{ChatEnabled: &no}, true},
		{"can sign up for free", &CopilotUser{ChatEnabled: &no, CanSignupForLimited: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CopilotEntitlementError(tt.user) != ""; got != tt.denied {
				t.Fatalf("denied = %v, want %v", got, tt.denied)
			}
		})
	}
}

func TestDiscoverCopilotAPIEndpointSendsVersionAndCaches(t *testing.T) {
	var calls atomic.Int32
	stubCopilotUser(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer gho_test" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != copilotUserAPIVersion {
			t.Errorf("X-GitHub-Api-Version = %q, want %q", got, copilotUserAPIVersion)
		}
		_, _ = w.Write([]byte(`{"chat_enabled":true,"endpoints":{"api":"https://api.business.githubcopilot.com"}}`))
	})

	for range 2 {
		if got := DiscoverCopilotAPIEndpoint(context.Background(), "gho_test", ""); got != "https://api.business.githubcopilot.com" {
			t.Fatalf("endpoint = %q", got)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("account endpoint called %d times, want 1 (cached)", got)
	}
}

func TestDiscoverCopilotAPIEndpointSkipsExchangedTokens(t *testing.T) {
	stubCopilotUser(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an exchanged Copilot API token must not be sent to api.github.com")
	})
	if got := DiscoverCopilotAPIEndpoint(context.Background(), "tid=abc;exp=1", ""); got != "" {
		t.Fatalf("endpoint = %q, want empty", got)
	}
}

func TestResolveCopilotAPIAccessUsesAccountEndpointWithRawToken(t *testing.T) {
	t.Setenv("PANDO_COPILOT_TOKEN_EXCHANGE", "0")
	stubCopilotUser(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"endpoints":{"api":"https://api.business.githubcopilot.com"}}`))
	})

	token, baseURL := ResolveCopilotAPIAccess(context.Background(), "gho_test", "", "")
	if token != "gho_test" {
		t.Errorf("token = %q, want the raw oauth token", token)
	}
	if baseURL != "https://api.business.githubcopilot.com" {
		t.Errorf("baseURL = %q, want the account's own host", baseURL)
	}

	// An explicitly configured host still wins over the advertised one.
	if _, baseURL := ResolveCopilotAPIAccess(context.Background(), "gho_test", "", "https://proxy.internal/copilot"); baseURL != "https://proxy.internal/copilot" {
		t.Errorf("baseURL = %q, want the configured host", baseURL)
	}
}
