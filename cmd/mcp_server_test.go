package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func TestEnsureMCPHTTPToken_ConfiguredTokenIsUsedAsIs(t *testing.T) {
	config.IsolateForTests(t)

	token, generated, err := ensureMCPHTTPToken("localhost", "configured-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generated {
		t.Error("generated = true, want false for an explicitly configured token")
	}
	if token != "configured-token" {
		t.Errorf("token = %q, want the configured value unchanged", token)
	}
}

func TestEnsureMCPHTTPToken_LoopbackWithNoTokenGeneratesOne(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "  localhost  "} {
		t.Run(host, func(t *testing.T) {
			// Each host gets its own isolated config dir so this covers
			// generation independently for every loopback spelling, rather
			// than the first subtest generating a token that every later
			// subtest would then just read back.
			config.IsolateForTests(t)

			token, generated, err := ensureMCPHTTPToken(host, "")
			if err != nil {
				t.Fatalf("unexpected error for loopback host %q: %v", host, err)
			}
			if !generated {
				t.Error("generated = false, want true when no token is configured")
			}
			if len(token) != 64 { // 32 random bytes, hex-encoded
				t.Errorf("token length = %d, want 64 hex characters", len(token))
			}
		})
	}
}

func TestEnsureMCPHTTPToken_NonLoopbackWithNoTokenIsRefused(t *testing.T) {
	config.IsolateForTests(t)

	for _, host := range []string{"0.0.0.0", "", "192.168.1.10", "example.com", "::"} {
		t.Run(host, func(t *testing.T) {
			_, _, err := ensureMCPHTTPToken(host, "")
			if err == nil {
				t.Fatalf("expected an error for non-loopback host %q with no token configured", host)
			}
			if !strings.Contains(err.Error(), "MCPServer.HttpToken") {
				t.Errorf("error %q does not name the config key MCPServer.HttpToken", err.Error())
			}
		})
	}
}

func TestEnsureMCPHTTPToken_NonLoopbackWithConfiguredTokenSucceeds(t *testing.T) {
	config.IsolateForTests(t)

	// A configured token is honored even on a non-loopback bind: the refusal
	// only guards against starting with NO authentication at all.
	token, generated, err := ensureMCPHTTPToken("0.0.0.0", "configured-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if generated {
		t.Error("generated = true, want false for an explicitly configured token")
	}
	if token != "configured-token" {
		t.Errorf("token = %q, want configured-token", token)
	}
}

// TestEnsureMCPHTTPToken_GeneratedTokensAreNotIdentical guards against a
// degenerate random generator. Two independently isolated config dirs are
// used deliberately: since PANDO-US-0030 made the token stable per config
// dir, two calls against the SAME dir are now expected to return the SAME
// token (see TestEnsureMCPHTTPToken_PersistsAndReusesTheStoredToken).
func TestEnsureMCPHTTPToken_GeneratedTokensAreNotIdentical(t *testing.T) {
	var tokenA, tokenB string

	t.Run("first", func(t *testing.T) {
		config.IsolateForTests(t)
		token, generated, err := ensureMCPHTTPToken("localhost", "")
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
		token, generated, err := ensureMCPHTTPToken("localhost", "")
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

// TestEnsureMCPHTTPToken_PersistsAndReusesTheStoredToken is the
// PANDO-US-0030 acceptance criterion for the MCP listener: a second start
// reads the same token back instead of generating a new one, and the file
// backing it has the required 0700/0600 modes.
func TestEnsureMCPHTTPToken_PersistsAndReusesTheStoredToken(t *testing.T) {
	config.IsolateForTests(t)

	first, generated, err := ensureMCPHTTPToken("localhost", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !generated {
		t.Fatal("generated = false, want true on the first start")
	}

	path, err := listenerTokenFilePath(mcpTokenKind)
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

	second, generatedAgain, err := ensureMCPHTTPToken("localhost", "")
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

// TestEnsureMCPHTTPToken_RefusesGroupOrWorldReadableStoredFile is the
// PANDO-US-0030 acceptance criterion: a token file with mode 0644 is refused
// with an error naming the file and its mode, not silently repaired.
func TestEnsureMCPHTTPToken_RefusesGroupOrWorldReadableStoredFile(t *testing.T) {
	config.IsolateForTests(t)

	path, err := listenerTokenFilePath(mcpTokenKind)
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

	_, _, err = ensureMCPHTTPToken("localhost", "")
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

// TestEnsureMCPHTTPToken_NonLoopbackRefusalIgnoresStoredFile is the
// PANDO-US-0030 acceptance criterion: a stored token does NOT satisfy the
// MCP non-loopback refusal, whether or not a stored file happens to be
// present from an earlier loopback start.
func TestEnsureMCPHTTPToken_NonLoopbackRefusalIgnoresStoredFile(t *testing.T) {
	config.IsolateForTests(t)

	if _, _, err := ensureMCPHTTPToken("localhost", ""); err != nil {
		t.Fatalf("unexpected error priming the stored token: %v", err)
	}

	_, _, err := ensureMCPHTTPToken("0.0.0.0", "")
	if err == nil {
		t.Fatal("expected an error for a non-loopback host, even with a stored token file present")
	}
	if !strings.Contains(err.Error(), "MCPServer.HttpToken") {
		t.Errorf("error %q does not name the config key MCPServer.HttpToken", err.Error())
	}
}

// TestRunMCPServerMode_PrintTokenFlagPrintsAndExitsWithoutStartingServer
// exercises the --print-token flag through runMCPServerMode itself: it must
// print the stored token and return before ever reaching db.Connect/app.New,
// which would otherwise fail or hang in a test environment.
func TestRunMCPServerMode_PrintTokenFlagPrintsAndExitsWithoutStartingServer(t *testing.T) {
	config.IsolateForTests(t)

	stored, _, err := resolveStoredOrGeneratedListenerToken(mcpTokenKind)
	if err != nil {
		t.Fatalf("unexpected error priming the stored token: %v", err)
	}

	if err := mcpServerCmd.Flags().Set("print-token", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	t.Cleanup(func() { _ = mcpServerCmd.Flags().Set("print-token", "false") })

	out, err := captureStdout(t, func() error { return runMCPServerMode(mcpServerCmd) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(out) != stored {
		t.Errorf("stdout = %q, want the stored token %q", strings.TrimSpace(out), stored)
	}
}

func TestIsLoopbackMCPHost(t *testing.T) {
	cases := map[string]bool{
		"localhost":   true,
		"LOCALHOST":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"[::1]":       true,
		"0.0.0.0":     false,
		"::":          false,
		"":            false,
		"example.com": false,
		"192.168.1.5": false,
	}
	for host, want := range cases {
		host, want := host, want
		t.Run(host, func(t *testing.T) {
			if got := isLoopbackMCPHost(host); got != want {
				t.Errorf("isLoopbackMCPHost(%q) = %v, want %v", host, got, want)
			}
		})
	}
}
