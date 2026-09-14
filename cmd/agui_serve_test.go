package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
// degenerate randomToken implementation.
func TestResolveAGUIToken_GeneratedTokensAreNotIdentical(t *testing.T) {
	tokenA, _, err := resolveAGUIToken("", false, "", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tokenB, _, err := resolveAGUIToken("", false, "", false, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenA == tokenB {
		t.Fatal("two independently generated tokens must not collide")
	}
}
