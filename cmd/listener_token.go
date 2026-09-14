package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// PANDO-US-0030: both listeners (AG-UI and MCP HTTP) generate a bearer token
// when none is explicitly configured, and used to print it exactly once to a
// stream (MCP to stderr, AG-UI to stdout) and forget it -- unrecoverable
// under a supervisor or a scrolled terminal. This file makes that token
// stable: generate it once, persist it under the user's global config
// directory, and read it back on every later start so a client configured
// once keeps working across restarts. The two listener kinds share this one
// mechanism and file format; only the file's basename differs.

// mcpTokenKind and aguiTokenKind name the two listener kinds this file
// persists a token for, and double as the stored file's basename suffix (see
// listenerTokenFilePath): "mcp-token" and "agui-token".
const (
	mcpTokenKind  = "mcp"
	aguiTokenKind = "agui"
)

// listenerTokenFilePath returns the path to the persisted token file for the
// given listener kind, one file per kind under config.GlobalConfigDir()
// (XDG-aware: $XDG_CONFIG_HOME/pando or ~/.config/pando), e.g.
// ~/.config/pando/mcp-token or ~/.config/pando/agui-token.
func listenerTokenFilePath(kind string) (string, error) {
	dir := config.GlobalConfigDir()
	if dir == "" {
		return "", fmt.Errorf("could not determine the global config directory to store the %s listener token in (no $HOME and no $XDG_CONFIG_HOME)", kind)
	}
	return filepath.Join(dir, kind+"-token"), nil
}

// loadStoredListenerToken reads a previously persisted token file. It
// reports found=false, err=nil when the file does not exist yet, or exists
// but is empty or whitespace-only (treated as nothing stored, so a fresh
// token is generated and written in its place).
//
// A file that is group- or world-readable is refused outright, naming the
// file and its mode: a token any other user on the box can read is not a
// secret any more. The mode is never silently repaired -- that is for the
// operator to notice and fix (or remove the file to have a new one
// generated).
func loadStoredListenerToken(path string) (token string, found bool, err error) {
	info, statErr := os.Stat(path)
	if os.IsNotExist(statErr) {
		return "", false, nil
	}
	if statErr != nil {
		return "", false, statErr
	}
	if mode := info.Mode().Perm(); mode&0o044 != 0 {
		return "", false, fmt.Errorf(
			"stored token file %s has mode %04o, which is readable by the group or other users; refusing to use it -- fix its permissions (chmod 600 %s) or remove it to have a new token generated",
			path, mode, path,
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	token = strings.TrimSpace(string(data))
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

// storeListenerToken persists token to path, creating the parent directory
// (0700) if needed and writing the file itself 0600. The write is atomic:
// content lands in a temp file first, which is then renamed into place, so a
// reader never observes a partially written token file.
func storeListenerToken(path, token string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to finalize %s: %w", path, err)
	}
	return nil
}

// generateListenerToken produces a fresh random bearer token: 32 random
// bytes, hex-encoded to 64 characters. Shared by both listeners so a
// weakened or predictable generator is one bug to fix, not two.
func generateListenerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate a listener token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// resolveStoredOrGeneratedListenerToken implements the shared tail of both
// listeners' token precedence (PANDO-US-0030): once every explicit source
// (CLI flag, token file, environment variable, or config field, depending on
// the listener) has come back unset, read a previously generated token back
// from disk, or -- on this listener kind's first start -- generate one and
// persist it so every later start reuses the same value instead of printing
// a new, unrecoverable one. Callers must only reach this after ruling out
// every explicit source themselves: this function never looks at any of
// them, and an explicitly configured token must never be written here.
func resolveStoredOrGeneratedListenerToken(kind string) (token string, generated bool, err error) {
	path, err := listenerTokenFilePath(kind)
	if err != nil {
		return "", false, err
	}
	stored, found, err := loadStoredListenerToken(path)
	if err != nil {
		return "", false, err
	}
	if found {
		return stored, false, nil
	}
	token, err = generateListenerToken()
	if err != nil {
		return "", false, err
	}
	if err := storeListenerToken(path, token); err != nil {
		return "", false, err
	}
	return token, true, nil
}

// printStoredListenerToken implements --print-token: it prints ONLY the
// stored token for the given listener kind to stdout, followed by a newline,
// and returns a non-nil error (which cmd/root.go's Execute turns into a
// non-zero exit) when none is stored yet. It never starts a listener, never
// touches project config, and never generates a token -- this is strictly a
// way to read back what an earlier start already generated and persisted,
// not another way to obtain one.
func printStoredListenerToken(kind string) error {
	path, err := listenerTokenFilePath(kind)
	if err != nil {
		return err
	}
	token, found, err := loadStoredListenerToken(path)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no stored %s listener token at %s; start the listener once with no token configured to generate one", kind, path)
	}
	fmt.Println(token)
	return nil
}
