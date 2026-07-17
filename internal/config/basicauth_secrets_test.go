package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// TestBasicAuthPasswordsRoundTripThroughAge verifies that WebUI Access passwords
// are age-encrypted on the way to the config file and come back as plaintext,
// the same treatment provider API keys get.
func TestBasicAuthPasswordsRoundTripThroughAge(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	original := &Config{
		Server: APIServerConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    9999,
			BasicAuth: BasicAuthConfig{
				Enabled: true,
				Users: []BasicAuthUser{
					{Username: "admin", Password: "s3cr3t"},
					{Username: "ops", Password: "contraseña-ñ"},
				},
			},
		},
	}

	encrypted, err := encryptSensitiveConfigFields(original)
	if err != nil {
		t.Fatalf("encryptSensitiveConfigFields() error = %v", err)
	}

	for i, user := range encrypted.Server.BasicAuth.Users {
		if !strings.HasPrefix(user.Password, encryptedValuePrefix) {
			t.Fatalf("basic auth password %d was not encrypted: %q", i, user.Password)
		}
	}
	if strings.Contains(encrypted.Server.BasicAuth.Users[0].Password, "s3cr3t") {
		t.Fatal("plaintext password leaked into the persisted config")
	}
	// Encryption must not mutate the in-memory config the server authenticates against.
	if original.Server.BasicAuth.Users[0].Password != "s3cr3t" {
		t.Fatal("encryptSensitiveConfigFields mutated the source config")
	}

	if err := decryptSensitiveConfigFields(encrypted); err != nil {
		t.Fatalf("decryptSensitiveConfigFields() error = %v", err)
	}
	if got := encrypted.Server.BasicAuth.Users[0].Password; got != "s3cr3t" {
		t.Fatalf("expected round-tripped password %q, got %q", "s3cr3t", got)
	}
	if got := encrypted.Server.BasicAuth.Users[1].Password; got != "contraseña-ñ" {
		t.Fatalf("expected round-tripped password %q, got %q", "contraseña-ñ", got)
	}
	if got := encrypted.Server.BasicAuth.Users[1].Username; got != "ops" {
		t.Fatalf("expected usernames to stay in the clear, got %q", got)
	}
}

// TestUpdateServerPersistsBasicAuthUsers drives the real write path: UpdateServer
// must emit [[Server.BasicAuth.Users]] with encrypted passwords and parse back.
func TestUpdateServerPersistsBasicAuthUsers(t *testing.T) {
	resetAgeKeyTestState()
	t.Cleanup(resetAgeKeyTestState)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	workingDir := t.TempDir()
	configFile := filepath.Join(workingDir, ".pando.toml")
	if err := os.WriteFile(configFile, []byte("Debug = true\n"), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg = &Config{WorkingDir: workingDir}
	t.Cleanup(func() { cfg = nil })

	err := UpdateServer(APIServerConfig{
		Enabled: true,
		Host:    "0.0.0.0",
		Port:    9999,
		BasicAuth: BasicAuthConfig{
			Enabled: true,
			Users:   []BasicAuthUser{{Username: "admin", Password: "s3cr3t"}},
		},
	})
	if err != nil {
		t.Fatalf("UpdateServer() error = %v", err)
	}

	written, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(written), "[[Server.BasicAuth.Users]]") {
		t.Fatalf("expected an array of tables for users, got:\n%s", written)
	}
	if strings.Contains(string(written), "s3cr3t") {
		t.Fatalf("plaintext password written to disk:\n%s", written)
	}

	var reloaded Config
	if err := toml.Unmarshal(written, &reloaded); err != nil {
		t.Fatalf("re-parse written config: %v", err)
	}
	if len(reloaded.Server.BasicAuth.Users) != 1 || !reloaded.Server.BasicAuth.Enabled {
		t.Fatalf("basic auth block did not survive the round-trip: %+v", reloaded.Server.BasicAuth)
	}
	if err := decryptSensitiveConfigFields(&reloaded); err != nil {
		t.Fatalf("decryptSensitiveConfigFields() error = %v", err)
	}
	if got := reloaded.Server.BasicAuth.Users[0].Password; got != "s3cr3t" {
		t.Fatalf("expected the password to decrypt back, got %q", got)
	}

	// The in-memory config the middleware reads must keep the plaintext.
	if got := Get().Server.BasicAuth.Users[0].Password; got != "s3cr3t" {
		t.Fatalf("expected plaintext in memory, got %q", got)
	}
}
