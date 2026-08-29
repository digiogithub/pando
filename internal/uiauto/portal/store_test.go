package portal

import (
	"os"
	"testing"
)

// isolateGlobalConfigDir points XDG_CONFIG_HOME at a fresh temp directory
// for the duration of the test, so the restore-token store round-trip test
// never touches the real developer machine's ~/.config/pando.
func isolateGlobalConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func TestRestoreTokenRoundTrip(t *testing.T) {
	isolateGlobalConfigDir(t)

	rd, sc := LoadRestoreTokens()
	if rd != "" || sc != "" {
		t.Fatalf("expected no tokens on a fresh store, got (%q,%q)", rd, sc)
	}

	if err := SaveRestoreTokens("rd-token", "sc-token"); err != nil {
		t.Fatalf("SaveRestoreTokens: %v", err)
	}

	rd, sc = LoadRestoreTokens()
	if rd != "rd-token" || sc != "sc-token" {
		t.Fatalf("LoadRestoreTokens = (%q,%q), want (rd-token,sc-token)", rd, sc)
	}
}

func TestRestoreTokenRoundTrip_ClearingAToken(t *testing.T) {
	isolateGlobalConfigDir(t)

	if err := SaveRestoreTokens("rd-token", "sc-token"); err != nil {
		t.Fatalf("SaveRestoreTokens: %v", err)
	}
	// A rejected/expired ScreenCast token should be clearable without
	// disturbing the RemoteDesktop token.
	if err := SaveRestoreTokens("rd-token", ""); err != nil {
		t.Fatalf("SaveRestoreTokens: %v", err)
	}
	rd, sc := LoadRestoreTokens()
	if rd != "rd-token" || sc != "" {
		t.Fatalf("LoadRestoreTokens = (%q,%q), want (rd-token,\"\")", rd, sc)
	}
}

func TestLoadRestoreTokens_UnreadableStoreDegradesToEmpty(t *testing.T) {
	isolateGlobalConfigDir(t)
	if err := SaveRestoreTokens("rd", "sc"); err != nil {
		t.Fatalf("SaveRestoreTokens: %v", err)
	}
	path := tokenStorePath()
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("corrupt the store file: %v", err)
	}
	rd, sc := LoadRestoreTokens()
	if rd != "" || sc != "" {
		t.Fatalf("expected corrupt store to degrade to empty tokens, got (%q,%q)", rd, sc)
	}
}
