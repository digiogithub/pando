package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// writeAGUITokenFile is a small test helper: writes content to a fresh file
// under t.TempDir() and returns its path.
func writeAGUITokenFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

// TestResolveAGUIToken_Precedence is the PANDO-US-0023 acceptance criterion:
// --token > --token-file > PANDO_AGUI_TOKEN > generated, exercised as a table
// so every override combination is covered in one place.
func TestResolveAGUIToken_Precedence(t *testing.T) {
	// Only the "nothing supplied generates one" case below reaches the
	// stored-file/generate tail (PANDO-US-0030); every other case returns
	// from an explicit source first. One isolated config dir shared across
	// the table is therefore enough -- it is written to at most once.
	config.IsolateForTests(t)

	fileWithToken := writeAGUITokenFile(t, "file-token")
	fileWithTrailingNewline := writeAGUITokenFile(t, "file-token-nl\n")

	tests := []struct {
		name string

		flagToken string
		flagSet   bool

		tokenFile    string
		tokenFileSet bool

		envToken string
		envSet   bool

		wantToken     string
		wantGenerated bool
		wantErr       bool
	}{
		{
			name:          "flag wins over file and env",
			flagToken:     "flag-token",
			flagSet:       true,
			tokenFile:     fileWithToken,
			tokenFileSet:  true,
			envToken:      "env-token",
			envSet:        true,
			wantToken:     "flag-token",
			wantGenerated: false,
		},
		{
			name:          "file wins over env when no flag",
			tokenFile:     fileWithToken,
			tokenFileSet:  true,
			envToken:      "env-token",
			envSet:        true,
			wantToken:     "file-token",
			wantGenerated: false,
		},
		{
			name:          "file content trims a trailing newline",
			tokenFile:     fileWithTrailingNewline,
			tokenFileSet:  true,
			wantToken:     "file-token-nl",
			wantGenerated: false,
		},
		{
			name:          "env used when no flag or file",
			envToken:      "env-token",
			envSet:        true,
			wantToken:     "env-token",
			wantGenerated: false,
		},
		{
			name:          "nothing supplied generates one",
			wantGenerated: true,
		},
		{
			name:         "missing token file is an error, not a fallback to generated",
			tokenFile:    filepath.Join(t.TempDir(), "does-not-exist"),
			tokenFileSet: true,
			envToken:     "env-token",
			envSet:       true,
			wantErr:      true,
		},
		{
			name:         "empty token file is an error, not a fallback to generated",
			tokenFile:    writeAGUITokenFile(t, "   \n"),
			tokenFileSet: true,
			envToken:     "env-token",
			envSet:       true,
			wantErr:      true,
		},
		{
			name:      "explicitly empty --token is an error, not a fallback",
			flagToken: "",
			flagSet:   true,
			envToken:  "env-token",
			envSet:    true,
			wantErr:   true,
		},
		{
			name:    "explicitly empty PANDO_AGUI_TOKEN is an error, not a fallback",
			envSet:  true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, generated, err := resolveAGUIToken(
				tc.flagToken, tc.flagSet,
				tc.tokenFile, tc.tokenFileSet,
				tc.envToken, tc.envSet,
			)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got token=%q generated=%v", token, generated)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if generated != tc.wantGenerated {
				t.Fatalf("generated = %v, want %v", generated, tc.wantGenerated)
			}
			if tc.wantGenerated {
				if len(token) != 64 { // 32 random bytes, hex-encoded
					t.Fatalf("generated token length = %d, want 64 hex characters", len(token))
				}
				return
			}
			if token != tc.wantToken {
				t.Fatalf("token = %q, want %q", token, tc.wantToken)
			}
		})
	}
}

// TestResolveAGUIToken_MissingFileNamesThePath guards that the error is
// actionable: an operator reading agui-serve's stderr must be able to tell
// which path was wrong.
func TestResolveAGUIToken_MissingFileNamesThePath(t *testing.T) {
	config.IsolateForTests(t)

	missing := filepath.Join(t.TempDir(), "nope")
	_, _, err := resolveAGUIToken("", false, missing, true, "", false)
	if err == nil {
		t.Fatal("expected an error for an unreadable --token-file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error %q does not name the missing path %q", err.Error(), missing)
	}
}

// TestResolveAGUIToken_GeneratedTokensAreNotIdentical guards against a
// degenerate random generator. Two independently isolated config dirs are
// used deliberately: since PANDO-US-0030 made the token stable per config
// dir, two calls against the SAME dir are now expected to return the SAME
// token (see TestResolveAGUIToken_PersistsAndReusesTheStoredToken).
func TestResolveAGUIToken_GeneratedTokensAreNotIdentical(t *testing.T) {
	var tokenA, tokenB string

	t.Run("first", func(t *testing.T) {
		config.IsolateForTests(t)
		token, generated, err := resolveAGUIToken("", false, "", false, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !generated {
			t.Fatal("generated = false, want true on a fresh config dir")
		}
		tokenA = token
	})
	t.Run("second", func(t *testing.T) {
		config.IsolateForTests(t)
		token, generated, err := resolveAGUIToken("", false, "", false, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !generated {
			t.Fatal("generated = false, want true on a fresh config dir")
		}
		tokenB = token
	})

	if tokenA == "" || tokenB == "" {
		t.Fatal("subtests did not run")
	}
	if tokenA == tokenB {
		t.Fatal("two independently generated tokens must not collide")
	}
}

// TestResolveAGUIToken_PersistsAndReusesTheStoredToken is the
// PANDO-US-0030 acceptance criterion for the AG-UI listener: a second start
// reads the same token back instead of generating a new one, and the file
// backing it has the required 0700/0600 modes.
func TestResolveAGUIToken_PersistsAndReusesTheStoredToken(t *testing.T) {
	config.IsolateForTests(t)

	first, generated, err := resolveAGUIToken("", false, "", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true on the first start")
	}

	path, err := listenerTokenFilePath(aguiTokenKind)
	if err != nil {
		t.Fatalf("listenerTokenFilePath: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat token file: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %04o, want 0600", perm)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("stat token dir: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("token dir mode = %04o, want 0700", perm)
	}

	second, generatedAgain, err := resolveAGUIToken("", false, "", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error on second start: %v", err)
	}
	if generatedAgain {
		t.Error("generated = true on the second start, want false (stable token read back)")
	}
	if second != first {
		t.Errorf("second start returned %q, want the stored token %q", second, first)
	}
}

// TestResolveAGUIToken_ExplicitTokenNeverWrittenToStoredFile is the
// PANDO-US-0030 acceptance criterion: an explicitly configured token wins
// over the stored file and is never written to it.
func TestResolveAGUIToken_ExplicitTokenNeverWrittenToStoredFile(t *testing.T) {
	config.IsolateForTests(t)

	stored, generated, err := resolveAGUIToken("", false, "", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error priming the stored token: %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true on the first start")
	}

	explicit, generatedAgain, err := resolveAGUIToken("explicit-token", true, "", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generatedAgain {
		t.Error("generated = true for an explicitly configured token")
	}
	if explicit != "explicit-token" {
		t.Errorf("token = %q, want the explicit value", explicit)
	}

	path, err := listenerTokenFilePath(aguiTokenKind)
	if err != nil {
		t.Fatalf("listenerTokenFilePath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored token file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != stored {
		t.Errorf("stored token file changed after an explicit token was used: got %q, want unchanged %q", got, stored)
	}
}

// TestResolveAGUIToken_RefusesGroupOrWorldReadableStoredFile is the
// PANDO-US-0030 acceptance criterion: a token file with mode 0644 is refused
// with an error naming the file and its mode, not silently repaired.
func TestResolveAGUIToken_RefusesGroupOrWorldReadableStoredFile(t *testing.T) {
	config.IsolateForTests(t)

	path, err := listenerTokenFilePath(aguiTokenKind)
	if err != nil {
		t.Fatalf("listenerTokenFilePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("leaked-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, _, err = resolveAGUIToken("", false, "", false, "", false)
	if err == nil {
		t.Fatal("expected an error for a group/world-readable stored token file")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the file %q", err.Error(), path)
	}
	if !strings.Contains(err.Error(), "0644") {
		t.Errorf("error %q does not name the mode 0644", err.Error())
	}
}

// TestRunAGUIServe_PrintTokenFlagPrintsAndExitsWithoutStartingServer
// exercises the --print-token flag through runAGUIServe itself: it must
// print the stored token and return before ever reaching db.Connect/app.New,
// which would otherwise fail or hang in a test environment.
func TestRunAGUIServe_PrintTokenFlagPrintsAndExitsWithoutStartingServer(t *testing.T) {
	config.IsolateForTests(t)

	stored, _, err := resolveStoredOrGeneratedListenerToken(aguiTokenKind)
	if err != nil {
		t.Fatalf("unexpected error priming the stored token: %v", err)
	}

	if err := aguiServeCmd.Flags().Set("print-token", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	t.Cleanup(func() { _ = aguiServeCmd.Flags().Set("print-token", "false") })

	out, err := captureStdout(t, func() error { return runAGUIServe(aguiServeCmd, nil) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != stored {
		t.Errorf("stdout = %q, want the stored token %q", strings.TrimSpace(out), stored)
	}
}
