package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to it, alongside fn's own return value. Used to assert
// that --print-token (and the low-level printStoredListenerToken it calls)
// write ONLY the token to stdout.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	fnErr := fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String(), fnErr
}

func TestListenerTokenFilePath_UsesGlobalConfigDirAndDistinctBasenames(t *testing.T) {
	config.IsolateForTests(t)

	mcpPath, err := listenerTokenFilePath(mcpTokenKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(mcpPath) != "mcp-token" {
		t.Errorf("mcp token file basename = %q, want %q", filepath.Base(mcpPath), "mcp-token")
	}
	if filepath.Dir(mcpPath) != config.GlobalConfigDir() {
		t.Errorf("mcp token file dir = %q, want %q", filepath.Dir(mcpPath), config.GlobalConfigDir())
	}

	aguiPath, err := listenerTokenFilePath(aguiTokenKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(aguiPath) != "agui-token" {
		t.Errorf("agui token file basename = %q, want %q", filepath.Base(aguiPath), "agui-token")
	}
	if mcpPath == aguiPath {
		t.Error("MCP and AG-UI must use distinct token files")
	}
}

func TestLoadStoredListenerToken_NotFoundWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	token, found, err := loadStoredListenerToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found || token != "" {
		t.Errorf("found = %v, token = %q, want false/empty for a missing file", found, token)
	}
}

func TestLoadStoredListenerToken_EmptyFileTreatedAsNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	token, found, err := loadStoredListenerToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found || token != "" {
		t.Errorf("found = %v, token = %q, want false/empty for a whitespace-only file", found, token)
	}
}

func TestLoadStoredListenerToken_TrimsWhitespaceAndAcceptsOwnerOnlyMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("a-token\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	token, found, err := loadStoredListenerToken(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || token != "a-token" {
		t.Errorf("found = %v, token = %q, want true/%q", found, token, "a-token")
	}
}

// TestLoadStoredListenerToken_RefusesGroupOrWorldReadableFile is the
// PANDO-US-0030 acceptance criterion: a token file with mode 0644 (or any
// other mode readable by the group or other users) is refused with an error
// naming the file and its mode -- the mode is never silently repaired.
func TestLoadStoredListenerToken_RefusesGroupOrWorldReadableFile(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o664, 0o666} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte("a-token\n"), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			_, found, err := loadStoredListenerToken(path)
			if err == nil {
				t.Fatalf("expected an error for mode %04o, got found=%v", mode, found)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the file %q", err.Error(), path)
			}
			wantMode := fmt.Sprintf("%04o", mode.Perm())
			if !strings.Contains(err.Error(), wantMode) {
				t.Errorf("error %q does not name the mode %s", err.Error(), wantMode)
			}
		})
	}
}

// TestLoadStoredListenerToken_OwnerExecuteOrWriteBitsAreIgnored guards the
// precise reading of "group- or world-readable": bits that are not the READ
// bit (e.g. group/other execute) do not trigger the refusal.
func TestLoadStoredListenerToken_OwnerExecuteOrWriteBitsAreIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("a-token\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o611); err != nil { // rw--------x--x, no read for group/other
		t.Fatalf("chmod: %v", err)
	}
	if _, found, err := loadStoredListenerToken(path); err != nil || !found {
		t.Fatalf("found = %v, err = %v, want true/nil for a file with no group/other READ bit", found, err)
	}
}

func TestStoreListenerToken_CreatesDirAndFileWithSecureModes(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "config-dir")
	path := filepath.Join(dir, "mcp-token")

	if err := storeListenerToken(path, "a-token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %04o, want 0700", perm)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %04o, want 0600", perm)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file %s.tmp left behind after store", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.TrimSpace(string(data)) != "a-token" {
		t.Errorf("stored content = %q, want %q", strings.TrimSpace(string(data)), "a-token")
	}
}

func TestResolveStoredOrGeneratedListenerToken_GeneratesThenReuses(t *testing.T) {
	config.IsolateForTests(t)

	first, generated, err := resolveStoredOrGeneratedListenerToken(mcpTokenKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true on the first call")
	}
	if len(first) != 64 { // 32 random bytes, hex-encoded
		t.Errorf("token length = %d, want 64 hex characters", len(first))
	}

	second, generatedAgain, err := resolveStoredOrGeneratedListenerToken(mcpTokenKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generatedAgain {
		t.Error("generated = true on the second call, want false (read back from disk)")
	}
	if second != first {
		t.Errorf("second call returned %q, want the stored token %q", second, first)
	}
}

func TestResolveStoredOrGeneratedListenerToken_KindsAreIndependent(t *testing.T) {
	config.IsolateForTests(t)

	mcpToken, _, err := resolveStoredOrGeneratedListenerToken(mcpTokenKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	aguiToken, _, err := resolveStoredOrGeneratedListenerToken(aguiTokenKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mcpToken == aguiToken {
		t.Error("MCP and AG-UI listener tokens must not be the same value")
	}
}

func TestPrintStoredListenerToken_ErrorsWhenNoneStored(t *testing.T) {
	config.IsolateForTests(t)

	out, err := captureStdout(t, func() error { return printStoredListenerToken(mcpTokenKind) })
	if err == nil {
		t.Fatal("expected an error when no token is stored")
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("stdout = %q, want empty on error", out)
	}
}

func TestPrintStoredListenerToken_PrintsOnlyTheToken(t *testing.T) {
	config.IsolateForTests(t)

	stored, _, err := resolveStoredOrGeneratedListenerToken(aguiTokenKind)
	if err != nil {
		t.Fatalf("unexpected error priming the stored token: %v", err)
	}

	out, err := captureStdout(t, func() error { return printStoredListenerToken(aguiTokenKind) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != stored {
		t.Errorf("stdout = %q, want exactly the stored token %q", strings.TrimSpace(out), stored)
	}
}
