package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

func resetAgeKeyTestState() {
	cfg = nil
	SetAgeKeysOverride("")
}

func TestEncryptDecryptSensitiveConfigFields(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cfg := &Config{
		ProviderAccounts: []ProviderAccount{
			{
				ID:                "antigravity-work",
				Type:              models.ProviderAntigravity,
				OAuthRefreshToken: "refresh-secret",
				OAuthAccessToken:  "access-secret",
				OAuthExpiry:       1735689600,
				ProjectID:         "project-123",
				Email:             "dev@example.com",
			},
		},
		MCPServers: map[string]MCPServer{
			"demo": {
				Env:     []string{"TOKEN=plain-token", "DEBUG=true"},
				Headers: map[string]string{"Authorization": "Bearer abc123"},
				Auth: &MCPAuth{
					Type:     MCPAuthBasic,
					Username: "alice",
					Password: "auth-secret",
					OAuth: &MCPOAuthConfig{
						ClientID:     "client-id-plain",
						ClientSecret: "oauth-client-secret",
					},
				},
			},
		},
		InternalTools: InternalToolsConfig{
			GoogleAPIKey:         "google-secret",
			GoogleSearchEngineID: "engine-id",
			BraveAPIKey:          "brave-secret",
			PerplexityAPIKey:     "pplx-secret",
			ExaAPIKey:            "exa-secret",
		},
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {APIKey: "anthropic-secret"},
			models.ProviderOpenAI:    {APIKey: "openai-secret"},
		},
		Remembrances: RemembrancesConfig{
			DocumentEmbeddingAPIKey: "doc-embed-secret",
			CodeEmbeddingAPIKey:     "code-embed-secret",
		},
	}

	encrypted, err := encryptSensitiveConfigFields(cfg)
	if err != nil {
		t.Fatalf("encryptSensitiveConfigFields() error = %v", err)
	}
	if encrypted.InternalTools.GoogleAPIKey == cfg.InternalTools.GoogleAPIKey {
		t.Fatal("google api key was not encrypted")
	}
	if !strings.Contains(encrypted.MCPServers["demo"].Env[0], encryptedValuePrefix) {
		t.Fatal("MCP env value was not encrypted")
	}
	if !strings.Contains(encrypted.MCPServers["demo"].Env[1], encryptedValuePrefix) {
		t.Fatal("MCP env value was not encrypted for non-secret text parameters")
	}
	if !strings.HasPrefix(encrypted.MCPServers["demo"].Headers["Authorization"], encryptedValuePrefix) {
		t.Fatal("MCP header was not encrypted")
	}
	demoAuth := encrypted.MCPServers["demo"].Auth
	if demoAuth == nil {
		t.Fatal("MCP auth block was dropped by encryption")
	}
	if !strings.HasPrefix(demoAuth.Password, encryptedValuePrefix) {
		t.Fatal("MCP auth password was not encrypted")
	}
	if demoAuth.Username != "alice" {
		t.Fatal("MCP auth username should remain in clear")
	}
	if demoAuth.OAuth == nil {
		t.Fatal("MCP auth OAuth block was dropped by encryption")
	}
	if !strings.HasPrefix(demoAuth.OAuth.ClientSecret, encryptedValuePrefix) {
		t.Fatal("MCP auth OAuth client secret was not encrypted")
	}
	if demoAuth.OAuth.ClientID != "client-id-plain" {
		t.Fatal("MCP auth OAuth client ID should remain in clear")
	}
	if !strings.HasPrefix(encrypted.Providers[models.ProviderAnthropic].APIKey, encryptedValuePrefix) {
		t.Fatal("anthropic provider API key was not encrypted")
	}
	if !strings.HasPrefix(encrypted.Providers[models.ProviderOpenAI].APIKey, encryptedValuePrefix) {
		t.Fatal("openai provider API key was not encrypted")
	}
	if !strings.HasPrefix(encrypted.ProviderAccounts[0].OAuthRefreshToken, encryptedValuePrefix) {
		t.Fatal("provider account OAuth refresh token was not encrypted")
	}
	if !strings.HasPrefix(encrypted.ProviderAccounts[0].OAuthAccessToken, encryptedValuePrefix) {
		t.Fatal("provider account OAuth access token was not encrypted")
	}
	if !strings.HasPrefix(encrypted.Remembrances.DocumentEmbeddingAPIKey, encryptedValuePrefix) {
		t.Fatal("document embedding API key was not encrypted")
	}
	if !strings.HasPrefix(encrypted.Remembrances.CodeEmbeddingAPIKey, encryptedValuePrefix) {
		t.Fatal("code embedding API key was not encrypted")
	}

	if err := decryptSensitiveConfigFields(encrypted); err != nil {
		t.Fatalf("decryptSensitiveConfigFields() error = %v", err)
	}
	if encrypted.InternalTools.GoogleAPIKey != "google-secret" || encrypted.InternalTools.ExaAPIKey != "exa-secret" {
		t.Fatal("internal tools secrets were not restored after decryption")
	}
	if encrypted.MCPServers["demo"].Env[0] != "TOKEN=plain-token" {
		t.Fatalf("unexpected decrypted env: %q", encrypted.MCPServers["demo"].Env[0])
	}
	if encrypted.MCPServers["demo"].Env[1] != "DEBUG=true" {
		t.Fatalf("unexpected decrypted non-secret env: %q", encrypted.MCPServers["demo"].Env[1])
	}
	if encrypted.MCPServers["demo"].Headers["Authorization"] != "Bearer abc123" {
		t.Fatal("MCP header was not restored after decryption")
	}
	restoredAuth := encrypted.MCPServers["demo"].Auth
	if restoredAuth == nil {
		t.Fatal("MCP auth block was dropped by decryption")
	}
	if restoredAuth.Password != "auth-secret" {
		t.Fatalf("MCP auth password was not restored: %q", restoredAuth.Password)
	}
	if restoredAuth.Username != "alice" {
		t.Fatalf("MCP auth username changed unexpectedly: %q", restoredAuth.Username)
	}
	if restoredAuth.OAuth == nil || restoredAuth.OAuth.ClientSecret != "oauth-client-secret" {
		t.Fatal("MCP auth OAuth client secret was not restored")
	}
	if restoredAuth.OAuth.ClientID != "client-id-plain" {
		t.Fatalf("MCP auth OAuth client ID changed unexpectedly: %q", restoredAuth.OAuth.ClientID)
	}
	if encrypted.Providers[models.ProviderAnthropic].APIKey != "anthropic-secret" {
		t.Fatal("anthropic provider API key was not restored after decryption")
	}
	if encrypted.Providers[models.ProviderOpenAI].APIKey != "openai-secret" {
		t.Fatal("openai provider API key was not restored after decryption")
	}
	if encrypted.ProviderAccounts[0].OAuthRefreshToken != "refresh-secret" {
		t.Fatal("provider account OAuth refresh token was not restored after decryption")
	}
	if encrypted.ProviderAccounts[0].OAuthAccessToken != "access-secret" {
		t.Fatal("provider account OAuth access token was not restored after decryption")
	}
	if encrypted.ProviderAccounts[0].ProjectID != "project-123" {
		t.Fatal("provider account project ID should remain unchanged")
	}
	if encrypted.ProviderAccounts[0].Email != "dev@example.com" {
		t.Fatal("provider account email should remain unchanged")
	}
	if encrypted.Remembrances.DocumentEmbeddingAPIKey != "doc-embed-secret" {
		t.Fatal("document embedding API key was not restored after decryption")
	}
	if encrypted.Remembrances.CodeEmbeddingAPIKey != "code-embed-secret" {
		t.Fatal("code embedding API key was not restored after decryption")
	}
}

func TestLoadOrCreateAgeKeyManagerStoresKeysInUserPandoPath(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	_, err := loadOrCreateAgeKeyManager()
	if err != nil {
		t.Fatalf("loadOrCreateAgeKeyManager() error = %v", err)
	}

	keyDir, publicKeyPath, privateKeyPath, err := pandoAgeKeyPaths()
	if err != nil {
		t.Fatalf("pandoAgeKeyPaths() error = %v", err)
	}
	if !strings.HasPrefix(keyDir, filepath.Join(home, ".config", appName)) {
		t.Fatalf("key dir %q not under user pando path", keyDir)
	}
	if _, err := os.Stat(publicKeyPath); err != nil {
		t.Fatalf("public key not created: %v", err)
	}
	if _, err := os.Stat(privateKeyPath); err != nil {
		t.Fatalf("private key not created: %v", err)
	}
}

func TestTransformSecretString(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	encrypted, err := TransformSecretString("super-secret")
	if err != nil {
		t.Fatalf("TransformSecretString() encrypt error = %v", err)
	}
	if !strings.HasPrefix(encrypted, encryptedValuePrefix) {
		t.Fatalf("expected encrypted value prefix, got %q", encrypted)
	}

	decrypted, err := TransformSecretString(encrypted)
	if err != nil {
		t.Fatalf("TransformSecretString() decrypt error = %v", err)
	}
	if decrypted != "super-secret" {
		t.Fatalf("expected decrypted value to round-trip, got %q", decrypted)
	}
}

func TestResolveMCPServerSecrets(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	encryptedArg, err := encryptSecretString("--token=secret")
	if err != nil {
		t.Fatalf("encryptSecretString(arg) error = %v", err)
	}
	encryptedEnv, err := encryptSecretString("plain-token")
	if err != nil {
		t.Fatalf("encryptSecretString(env) error = %v", err)
	}
	encryptedHeader, err := encryptSecretString("Bearer abc123")
	if err != nil {
		t.Fatalf("encryptSecretString(header) error = %v", err)
	}
	encryptedCommand, err := encryptSecretString("/usr/bin/demo")
	if err != nil {
		t.Fatalf("encryptSecretString(command) error = %v", err)
	}
	encryptedURL, err := encryptSecretString("https://example.com/mcp")
	if err != nil {
		t.Fatalf("encryptSecretString(url) error = %v", err)
	}

	server, err := ResolveMCPServerSecrets(MCPServer{
		Command: encryptedCommand,
		Args:    []string{encryptedArg, "--verbose"},
		Env:     []string{"TOKEN=" + encryptedEnv, "DEBUG=true"},
		URL:     encryptedURL,
		Headers: map[string]string{"Authorization": encryptedHeader},
	})
	if err != nil {
		t.Fatalf("ResolveMCPServerSecrets() error = %v", err)
	}
	if server.Command != "/usr/bin/demo" {
		t.Fatalf("unexpected command: %q", server.Command)
	}
	if server.Args[0] != "--token=secret" || server.Args[1] != "--verbose" {
		t.Fatalf("unexpected args: %#v", server.Args)
	}
	if server.Env[0] != "TOKEN=plain-token" || server.Env[1] != "DEBUG=true" {
		t.Fatalf("unexpected env: %#v", server.Env)
	}
	if server.URL != "https://example.com/mcp" {
		t.Fatalf("unexpected url: %q", server.URL)
	}
	if server.Headers["Authorization"] != "Bearer abc123" {
		t.Fatalf("unexpected headers: %#v", server.Headers)
	}
}

func TestResolveMCPServerSecretsWithAuth(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	encryptedToken, err := encryptSecretString("secret-token")
	if err != nil {
		t.Fatalf("encryptSecretString(token) error = %v", err)
	}
	encryptedClientSecret, err := encryptSecretString("secret-oauth")
	if err != nil {
		t.Fatalf("encryptSecretString(oauth secret) error = %v", err)
	}

	original := MCPServer{
		Auth: &MCPAuth{
			Type:  MCPAuthBearer,
			Token: encryptedToken,
			OAuth: &MCPOAuthConfig{
				ClientID:     "plain-client-id",
				ClientSecret: encryptedClientSecret,
			},
		},
	}

	resolved, err := ResolveMCPServerSecrets(original)
	if err != nil {
		t.Fatalf("ResolveMCPServerSecrets() error = %v", err)
	}
	if resolved.Auth.Token != "secret-token" {
		t.Fatalf("unexpected resolved auth token: %q", resolved.Auth.Token)
	}
	if resolved.Auth.OAuth.ClientSecret != "secret-oauth" {
		t.Fatalf("unexpected resolved oauth client secret: %q", resolved.Auth.OAuth.ClientSecret)
	}
	if resolved.Auth.OAuth.ClientID != "plain-client-id" {
		t.Fatalf("unexpected resolved oauth client id: %q", resolved.Auth.OAuth.ClientID)
	}

	// The original server's nested Auth/OAuth pointers must never be mutated.
	if original.Auth.Token != encryptedToken {
		t.Fatal("ResolveMCPServerSecrets mutated the original Auth.Token in place")
	}
	if original.Auth.OAuth.ClientSecret != encryptedClientSecret {
		t.Fatal("ResolveMCPServerSecrets mutated the original Auth.OAuth.ClientSecret in place")
	}
}

func TestNamedAgeKeySetsUseSeparateStorage(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	SetAgeKeysOverride("team-A")
	encrypted, err := TransformSecretString("shared-secret")
	if err != nil {
		t.Fatalf("TransformSecretString(team-A) error = %v", err)
	}
	teamDir, _, _, err := pandoAgeKeyPaths()
	if err != nil {
		t.Fatalf("pandoAgeKeyPaths(team-A) error = %v", err)
	}
	if !strings.HasSuffix(teamDir, filepath.Join("keys", "team-A")) {
		t.Fatalf("unexpected team key dir %q", teamDir)
	}

	SetAgeKeysOverride("team-B")
	otherDir, _, _, err := pandoAgeKeyPaths()
	if err != nil {
		t.Fatalf("pandoAgeKeyPaths(team-B) error = %v", err)
	}
	if !strings.HasSuffix(otherDir, filepath.Join("keys", "team-B")) {
		t.Fatalf("unexpected other key dir %q", otherDir)
	}
	if _, err := TransformSecretString(encrypted); err == nil {
		t.Fatal("expected decryption with a different named keypair to fail")
	}

	SetAgeKeysOverride("team-A")
	decrypted, err := TransformSecretString(encrypted)
	if err != nil {
		t.Fatalf("TransformSecretString(team-A decrypt) error = %v", err)
	}
	if decrypted != "shared-secret" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}
}
