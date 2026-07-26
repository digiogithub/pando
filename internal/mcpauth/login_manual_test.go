package mcpauth

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// TestManager_Login_ManualCode_FullURLStateValidates exercises the
// --manual-paste path (LoginPrompt.ManualCode) the way cmd/mcp.go's
// parseManualCodeInput feeds it after a user pastes the full redirect URL:
// code, state and (if present) iss are all extracted from the URL and must
// validate exactly like the HTTP callback path does.
func TestManager_Login_ManualCode_FullURLStateValidates(t *testing.T) {
	m := newTestManager(t)
	stub := stubAuthServer(t, false)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				ClientID:              "static-client-id",
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}

	var capturedAuthURL string
	prompt := LoginPrompt{
		OnAuthorizationURL: func(u string) { capturedAuthURL = u },
		ManualCode: func(ctx context.Context) (string, string, string, error) {
			u, err := url.Parse(capturedAuthURL)
			if err != nil {
				return "", "", "", err
			}
			return "stub-authorization-code", u.Query().Get("state"), u.Query().Get("iss"), nil
		},
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Login(ctx, "manual-full-url-server", srv, prompt); err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if !m.HasTokens("manual-full-url-server", srv.URL) {
		t.Fatal("expected tokens to be persisted")
	}
}

// TestManager_Login_ManualCode_StateMismatchRejected covers a pasted (or
// otherwise recovered) state that does not match the one Pando generated —
// this must be rejected as a possible CSRF regardless of the manual path
// being used instead of the HTTP callback.
func TestManager_Login_ManualCode_StateMismatchRejected(t *testing.T) {
	m := newTestManager(t)
	stub := stubAuthServer(t, false)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				ClientID:              "static-client-id",
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}

	prompt := LoginPrompt{
		OnAuthorizationURL: func(u string) {},
		ManualCode: func(ctx context.Context) (string, string, string, error) {
			return "stub-authorization-code", "definitely-the-wrong-state", "", nil
		},
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := m.Login(ctx, "manual-state-mismatch-server", srv, prompt)
	if err == nil {
		t.Fatal("expected an error on state mismatch")
	}
	if !strings.Contains(err.Error(), "CSRF") {
		t.Errorf("error = %v, want it to mention CSRF", err)
	}
	if m.HasTokens("manual-state-mismatch-server", srv.URL) {
		t.Fatal("no tokens should have been persisted on state mismatch")
	}
}

// TestManager_Login_ManualCode_BareCodeEmptyStateAccepted covers the
// deliberately weaker bare-code path: ManualCode returns an empty state
// (nothing to compare, since only the code was pasted) and Login must still
// accept it — the CLI layer (cmd/mcp.go) is responsible for gating this on
// an explicit user confirmation/--force before ever calling into Login this
// way; Manager.Login itself just implements the documented "empty state is
// accepted, non-empty state must match" contract.
func TestManager_Login_ManualCode_BareCodeEmptyStateAccepted(t *testing.T) {
	m := newTestManager(t)
	stub := stubAuthServer(t, false)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				ClientID:              "static-client-id",
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}

	prompt := LoginPrompt{
		OnAuthorizationURL: func(u string) {},
		ManualCode: func(ctx context.Context) (string, string, string, error) {
			return "stub-authorization-code", "", "", nil
		},
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Login(ctx, "manual-bare-code-server", srv, prompt); err != nil {
		t.Fatalf("Login returned error for the bare-code (empty state) path: %v", err)
	}
	if !m.HasTokens("manual-bare-code-server", srv.URL) {
		t.Fatal("expected tokens to be persisted")
	}
}

// TestManager_Login_ManualCode_IssFromPastedURLValidated proves the iss
// value ManualCode recovers from a pasted full redirect URL is actually fed
// into the same RFC 9207 validation the HTTP callback path uses, instead of
// being silently skipped — a mismatched iss must abort before token
// exchange exactly as it does on the callback path (see
// TestManager_Login_IssMismatchAbortsBeforeTokenExchange).
func TestManager_Login_ManualCode_IssFromPastedURLValidated(t *testing.T) {
	m := newTestManager(t)
	stub, tokenCalls := stubIssAuthServer(t, true)

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://example-resource.test/mcp",
		Auth: &config.MCPAuth{
			Type: config.MCPAuthOAuth,
			OAuth: &config.MCPOAuthConfig{
				ClientID:              "static-client-id",
				AuthServerMetadataURL: stub.URL + "/as-metadata",
				RedirectURI:           "http://127.0.0.1:0/mcp/oauth/callback",
			},
		},
	}

	var capturedAuthURL string
	prompt := LoginPrompt{
		OnAuthorizationURL: func(u string) { capturedAuthURL = u },
		ManualCode: func(ctx context.Context) (string, string, string, error) {
			u, err := url.Parse(capturedAuthURL)
			if err != nil {
				return "", "", "", err
			}
			// Simulate the user pasting the redirect URL with a mismatched
			// iss (as an attacker-controlled or misconfigured AS might send).
			return "stub-authorization-code", u.Query().Get("state"), "https://attacker.example.com", nil
		},
		Timeout: 5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := m.Login(ctx, "manual-iss-mismatch-server", srv, prompt)
	if err == nil {
		t.Fatal("expected an error on iss mismatch fed through the manual path")
	}
	if !strings.Contains(err.Error(), "mix-up") {
		t.Errorf("error = %v, want it to mention the possible mix-up attack", err)
	}
	if *tokenCalls != 0 {
		t.Fatalf("token endpoint calls = %d, want 0 (must abort before exchange)", *tokenCalls)
	}
	if m.HasTokens("manual-iss-mismatch-server", srv.URL) {
		t.Fatal("no tokens should have been persisted on iss mismatch")
	}
}
