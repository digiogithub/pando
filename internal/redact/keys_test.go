package redact

import "testing"

func TestIsSecretKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"apiKey", true},
		{"api_key", true},
		{"API-KEY", true},
		{"oauthAccessToken", true},
		{"OAUTH_ACCESS_TOKEN", true},
		{"sourcegraphToken", true},
		{"password", true},
		{"passwd", true},
		{"Authorization", true},
		{"x-api-key", true},
		{"Cookie", true},
		{"Set-Cookie", true},
		{"privateKey", true},
		{"private_key", true},
		{"credential", true},
		{"credentials", true},
		{"mcp.credentials", true},
		{"", false},
		{"id", false},
		{"baseUrl", false},
		{"promptTokens", false},
		{"maxOutputTokens", false},
		{"tokenOptimization", false},
		{"username", false},
	}
	for _, tc := range cases {
		if got := IsSecretKey(tc.key); got != tc.want {
			t.Errorf("IsSecretKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
