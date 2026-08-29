package portal

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/internal/config"
)

// This file persists the XDG desktop portal restore tokens (W3) in
// Pando's existing per-machine global config directory
// (internal/config.GlobalConfigDir(), $XDG_CONFIG_HOME/pando or
// ~/.config/pando — the same location internal/config/global_projects.go
// already uses for the global projects registry) rather than inventing a
// new location. Without this, SelectDevices/SelectSources re-prompt the
// user for consent on every single Manager/process run, which makes
// Wayland unusable for an unattended agent.

// tokenStoreFileName is the JSON file holding the persisted restore
// tokens, one per portal interface (RemoteDesktop and ScreenCast can each
// return their own).
const tokenStoreFileName = "uiauto_wayland_portal.json"

type tokenStore struct {
	RemoteDesktopRestoreToken string `json:"remoteDesktopRestoreToken,omitempty"`
	ScreenCastRestoreToken    string `json:"screenCastRestoreToken,omitempty"`
}

func tokenStorePath() string {
	dir := config.GlobalConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, tokenStoreFileName)
}

// LoadRestoreTokens returns the persisted RemoteDesktop/ScreenCast restore
// tokens, or two empty strings if none are stored yet (fresh machine, or
// the store is unavailable/unreadable — read failures are treated the same
// as "no token", never a hard error: the caller degrades to a fresh
// consent prompt).
func LoadRestoreTokens() (remoteDesktop, screenCast string) {
	path := tokenStorePath()
	if path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var st tokenStore
	if err := json.Unmarshal(data, &st); err != nil {
		return "", ""
	}
	return st.RemoteDesktopRestoreToken, st.ScreenCastRestoreToken
}

// SaveRestoreTokens atomically persists the given restore tokens, so the
// next Session.Open on this machine can pass them back and skip the
// consent dialog. Passing an empty string for either leaves that field
// cleared (a token was rejected/expired, or the compositor did not grant
// one).
func SaveRestoreTokens(remoteDesktop, screenCast string) error {
	path := tokenStorePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	st := tokenStore{RemoteDesktopRestoreToken: remoteDesktop, ScreenCastRestoreToken: screenCast}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
