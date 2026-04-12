package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptDecryptSensitiveConfigFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	cfg := &Config{
		MCPServers: map[string]MCPServer{
			"demo": {
				Env: []string{"TOKEN=plain-token", "DEBUG=true"},
				Headers: map[string]string{"Authorization": "Bearer abc123"},
			},
		},
		InternalTools: InternalToolsConfig{
			GoogleAPIKey:         "google-secret",
			GoogleSearchEngineID: "engine-id",
			BraveAPIKey:          "brave-secret",
			PerplexityAPIKey:     "pplx-secret",
			ExaAPIKey:            "exa-secret",
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
}

func TestLoadOrCreateAgeKeyManagerStoresKeysInUserPandoPath(t *testing.T) {
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
