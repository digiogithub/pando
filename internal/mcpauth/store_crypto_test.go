package mcpauth

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newIsolatedStore returns a Store backed by a fresh temp file, additionally
// pointing PANDO_MCP_AUTH_FILE at it (per the mandatory test convention for
// anything touching the store) even though New(path) with an explicit path
// never consults that env var itself — this guards against a future refactor
// that makes some code path fall back to the default resolution.
func newIsolatedStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	t.Setenv(envAuthFileOverride, path)
	return New(path), path
}

func TestStore_EncryptedAtRest_RoundTrip(t *testing.T) {
	s, path := newIsolatedStore(t)

	entry := &Entry{
		ServerURL: "https://example.com/mcp",
		Tokens: &Tokens{
			AccessToken:  "super-secret-access-token",
			TokenType:    "Bearer",
			RefreshToken: "super-secret-refresh-token",
			Scope:        "read write",
			ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
		},
		ClientInfo: &ClientInfo{ClientID: "client-1", ClientSecret: "super-secret-client-secret"},
	}
	if err := s.Set("myserver", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// The raw bytes on disk must never contain the plaintext secrets.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, secret := range []string{"super-secret-access-token", "super-secret-refresh-token", "super-secret-client-secret"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("on-disk file contains plaintext secret %q:\n%s", secret, raw)
		}
	}
	// Non-secret fields must stay readable in clear (per-value, not
	// whole-file, encryption).
	if !bytes.Contains(raw, []byte("https://example.com/mcp")) {
		t.Errorf("on-disk file should keep ServerURL in clear:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("client-1")) {
		t.Errorf("on-disk file should keep ClientID in clear:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("age1:")) {
		t.Errorf("on-disk file should contain the age1: envelope prefix for encrypted values:\n%s", raw)
	}

	// File permissions unaffected by encryption.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 0600", perm)
	}

	// Reading back through the Store transparently decrypts.
	got, ok, err := s.Get("myserver")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("expected entry to exist")
	}
	if got.Tokens == nil || got.Tokens.AccessToken != "super-secret-access-token" {
		t.Errorf("Tokens.AccessToken = %+v, want decrypted plaintext", got.Tokens)
	}
	if got.Tokens.RefreshToken != "super-secret-refresh-token" {
		t.Errorf("Tokens.RefreshToken = %q, want decrypted plaintext", got.Tokens.RefreshToken)
	}
	if got.ClientInfo == nil || got.ClientInfo.ClientSecret != "super-secret-client-secret" {
		t.Errorf("ClientInfo.ClientSecret = %+v, want decrypted plaintext", got.ClientInfo)
	}
}

func TestStore_LegacyPlaintextFile_StillLoads_AndUpgradesOnWrite(t *testing.T) {
	_, path := newIsolatedStore(t)

	// Simulate a pre-Phase-6 plaintext file written directly to disk (no
	// Store involved), the way an existing installation's file would look.
	legacy := fileFormat{
		Version: 1,
		Servers: map[string]Entry{
			"myserver": {
				ServerURL: "https://example.com/mcp",
				Tokens: &Tokens{
					AccessToken:  "legacy-plaintext-access-token",
					RefreshToken: "legacy-plaintext-refresh-token",
				},
				ClientInfo: &ClientInfo{ClientID: "client-1", ClientSecret: "legacy-plaintext-secret"},
			},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write legacy fixture: %v", err)
	}

	s := New(path)

	// The plaintext legacy file must still load correctly.
	got, ok, err := s.Get("myserver")
	if err != nil {
		t.Fatalf("Get on legacy plaintext file: %v", err)
	}
	if !ok || got.Tokens == nil || got.Tokens.AccessToken != "legacy-plaintext-access-token" {
		t.Fatalf("Get on legacy plaintext file = ok=%v entry=%+v", ok, got)
	}

	// Any write (even one that doesn't touch this entry) re-encrypts it.
	if err := s.Set("myserver", got); err != nil {
		t.Fatalf("Set (upgrade write): %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after upgrade write: %v", err)
	}
	if bytes.Contains(raw, []byte("legacy-plaintext-access-token")) {
		t.Errorf("file still contains plaintext access token after upgrade write:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("age1:")) {
		t.Errorf("file should be encrypted after upgrade write:\n%s", raw)
	}

	// And it must still read back correctly post-upgrade.
	got2, ok, err := s.Get("myserver")
	if err != nil || !ok {
		t.Fatalf("Get after upgrade write: ok=%v err=%v", ok, err)
	}
	if got2.Tokens.AccessToken != "legacy-plaintext-access-token" {
		t.Errorf("AccessToken after upgrade round-trip = %q, want unchanged plaintext value", got2.Tokens.AccessToken)
	}
}

// isolateUnwritableAgeKeyHome points $HOME at a directory whose
// "~/.config" subdirectory is not writable by the current (non-root) user,
// so internal/config's loadOrCreateAgeKeyManager cannot create the AGE key
// directory or write key material. This simulates "no AGE key available"
// (e.g. a fresh/misconfigured machine, a read-only config home) without
// needing root or any actual key-management API to inject the failure.
func isolateUnwritableAgeKeyHome(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("cannot simulate a permission-denied home directory while running as root")
	}
	home := t.TempDir()
	configDir := filepath.Join(home, ".config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatalf("chmod config dir read-only: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestStore_NoAgeKeyAvailable_FallsBackToPlaintext(t *testing.T) {
	isolateUnwritableAgeKeyHome(t)

	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	t.Setenv(envAuthFileOverride, path)
	s := New(path)

	entry := &Entry{
		ServerURL: "https://example.com/mcp",
		Tokens:    &Tokens{AccessToken: "fallback-plaintext-token"},
	}
	// Set must succeed (never a hard failure) even though AGE key material
	// cannot be created.
	if err := s.Set("myserver", entry); err != nil {
		t.Fatalf("Set must not fail when no AGE key is available: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(raw, []byte("fallback-plaintext-token")) {
		t.Errorf("expected plaintext fallback token in file, got:\n%s", raw)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 0600 even in fallback mode", perm)
	}

	// The store must still be fully usable end to end in fallback mode.
	got, ok, err := s.Get("myserver")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || got.Tokens == nil || got.Tokens.AccessToken != "fallback-plaintext-token" {
		t.Fatalf("Get after fallback Set = ok=%v entry=%+v", ok, got)
	}
}

func TestStore_CorruptEntry_DoesNotBreakOthers(t *testing.T) {
	_, path := newIsolatedStore(t)

	// One entry has a well-formed "age1:" prefix but garbage payload that
	// cannot possibly decrypt (wrong/foreign ciphertext); the other is
	// ordinary plaintext (as if written in fallback mode, or pre-upgrade).
	broken := fileFormat{
		Version: 1,
		Servers: map[string]Entry{
			"broken-server": {
				ServerURL: "https://broken.example.com/mcp",
				Tokens: &Tokens{
					AccessToken: "age1:" + "AAAAnotvalidage1ciphertextAAAA==",
				},
			},
			"healthy-server": {
				ServerURL: "https://healthy.example.com/mcp",
				Tokens:    &Tokens{AccessToken: "healthy-plaintext-token"},
			},
		},
	}
	data, err := json.Marshal(broken)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	s := New(path)

	all, err := s.All()
	if err != nil {
		t.Fatalf("All() must not fail on one corrupt entry: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("All() = %d entries, want 2 (both server names retained)", len(all))
	}

	brokenEntry := all["broken-server"]
	if brokenEntry.Tokens != nil {
		t.Errorf("broken-server Tokens = %+v, want nil (undecryptable tokens dropped, not the whole entry)", brokenEntry.Tokens)
	}
	if brokenEntry.ServerURL != "https://broken.example.com/mcp" {
		t.Errorf("broken-server ServerURL = %q, want preserved", brokenEntry.ServerURL)
	}

	healthyEntry := all["healthy-server"]
	if healthyEntry.Tokens == nil || healthyEntry.Tokens.AccessToken != "healthy-plaintext-token" {
		t.Errorf("healthy-server Tokens = %+v, want untouched", healthyEntry.Tokens)
	}
}

func TestStore_EncryptedValue_HasAgePrefix(t *testing.T) {
	s, path := newIsolatedStore(t)
	if err := s.UpdateTokens("srv", &Tokens{AccessToken: "at-value"}, "https://example.com"); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "\"accessToken\":\"at-value\"") {
		t.Errorf("accessToken was stored in clear:\n%s", raw)
	}
}
