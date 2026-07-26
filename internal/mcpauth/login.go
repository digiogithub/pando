package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"

	"github.com/mark3labs/mcp-go/client/transport"

	"golang.org/x/term"
)

// clientDisplayName identifies this client during dynamic client
// registration and on authorization-server consent screens.
const clientDisplayName = "Pando"

// LoginPrompt lets the caller (CLI today, TUI/WebUI later) drive the
// interactive parts of Login without Manager needing to know anything about
// terminals, browsers, or GUI dialogs.
type LoginPrompt struct {
	// OnAuthorizationURL is called once the authorization URL is ready,
	// whether or not the browser is opened automatically. Callers should
	// always print/display it so the user has a fallback if OpenBrowser
	// fails or is disabled.
	OnAuthorizationURL func(url string)

	// OpenBrowser, when true, makes Login attempt to open the authorization
	// URL in the user's default browser. A failure to open is reported via
	// OnBrowserError (if set) but never aborts the flow: OnAuthorizationURL
	// already gave the user the URL to open manually.
	OpenBrowser bool

	// OnBrowserError is called when OpenBrowser is true but launching the
	// browser failed. Optional.
	OnBrowserError func(err error)

	// ManualCode, when non-nil, is used instead of waiting on the local
	// callback server: it should block until the user has pasted back the
	// authorization code (or the full redirect URL) and the state Pando
	// generated, returning them so Login can validate and exchange them.
	// This is the fallback for headless/remote sessions where the local
	// callback server is unreachable from the browser that completes the
	// login.
	//
	// iss is the RFC 9207 §3 authorization-server issuer, when the caller was
	// able to parse it out of a pasted full redirect URL (its `iss` query
	// parameter); it is fed into the same validateIssuer check the HTTP
	// callback path uses. Return an empty iss (never an error) when only a
	// bare code was pasted and no iss is available to check — validateIssuer
	// treats that as "nothing to compare" unless the authorization server is
	// known (from cached metadata) to declare
	// authorization_response_iss_parameter_supported=true, in which case an
	// empty iss is correctly rejected rather than silently skipped.
	ManualCode func(ctx context.Context) (code string, state string, iss string, err error)

	// Timeout bounds how long Login waits for the callback (or ManualCode)
	// before giving up. Zero uses DefaultCallbackTimeout.
	Timeout time.Duration
}

// StatusInfo summarizes one server's OAuth state for `pando mcp status` and
// any future TUI/WebUI surface.
type StatusInfo struct {
	ServerName            string
	ServerURL             string
	AuthType              string
	HasTokens             bool
	ExpiresAt             time.Time
	Expired               bool
	ClientID              string
	DynamicallyRegistered bool
}

// IsInteractive reports whether stdin/stdout look like a usable terminal, so
// callers can decide between driving the interactive browser flow and
// failing fast with actionable instructions.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// NonInteractiveLoginError is returned by callers (agent/tool layer) that
// detect a server needs authorization but cannot drive an interactive
// login themselves (no TTY, running as a background tool call, etc.).
func NonInteractiveLoginError(serverName string) error {
	return fmt.Errorf("MCP server %q requires authorization and no interactive session is available. Run: pando mcp login %s", serverName, serverName)
}

// Login drives the full interactive OAuth 2.1 authorization-code + PKCE flow
// for serverName and persists the resulting tokens (and, if dynamic client
// registration happened, the client credentials) so future calls need no
// further interaction until the refresh token itself stops working.
//
// srv must be the server's config entry with secrets already resolved via
// config.ResolveMCPServerSecrets (the caller may pass the raw config from
// config.Get().MCPServers[serverName]; Login resolves it itself if not).
func (m *Manager) Login(ctx context.Context, serverName string, srv config.MCPServer, p LoginPrompt) error {
	if srv.Type == config.MCPStdio {
		return fmt.Errorf("mcpauth: server %q is a stdio server; stdio servers take credentials from the environment, not OAuth", serverName)
	}
	resolved, err := config.ResolveMCPServerSecrets(srv)
	if err != nil {
		return fmt.Errorf("mcpauth: resolve server %q secrets: %w", serverName, err)
	}
	if !resolved.Auth.IsInteractiveOAuth() {
		if resolved.Auth.ResolvedType() == config.MCPAuthOAuthClientCredentials {
			return fmt.Errorf("mcpauth: server %q uses the %q grant, which has no interactive flow; call LoginClientCredentials (or `pando mcp login %s`, which dispatches to it automatically)", serverName, config.MCPAuthOAuthClientCredentials, serverName)
		}
		return fmt.Errorf("mcpauth: server %q auth type is %q, not oauth; nothing to log in to", serverName, resolved.Auth.ResolvedType())
	}
	if strings.TrimSpace(resolved.URL) == "" {
		return fmt.Errorf("mcpauth: server %q has no URL configured", serverName)
	}

	oauthCfg, err := m.OAuthConfig(serverName, resolved)
	if err != nil {
		return fmt.Errorf("mcpauth: build oauth config for %q: %w", serverName, err)
	}

	// Accumulate scopes with any previously requested set (step-up auth —
	// see stepup.go): a fresh Login must not silently narrow the scope the
	// user already granted in an earlier session.
	oauthCfg.Scopes = m.StepUpScopes(serverName, oauthCfg.Scopes)

	// Phase 5: apply mTLS/TLS options (if any) to the HTTP client the
	// OAuthHandler uses for its own metadata/registration/token-exchange
	// requests. A nil tlsCfg leaves oauthCfg.HTTPClient untouched so
	// mcp-go's own 30s-timeout default client is used, exactly as before
	// this field existed.
	tlsCfg, err := BuildTLSConfig(resolved)
	if err != nil {
		return fmt.Errorf("mcpauth: %w", err)
	}
	if tlsCfg != nil {
		oauthCfg.HTTPClient = HTTPClientForTLS(tlsCfg, 30*time.Second)
	}

	callbackSrv, err := StartCallbackServer(oauthCfg.RedirectURI)
	if err != nil {
		return fmt.Errorf("mcpauth: start local callback server: %w", err)
	}
	defer callbackSrv.Close()

	// Use the redirect URI the callback server actually bound to (it may
	// differ from the configured one, e.g. port 0 resolving to an ephemeral
	// port), so the authorization request and the token exchange agree with
	// where the server is really listening.
	oauthCfg.RedirectURI = callbackSrv.RedirectURI()

	handler := transport.NewOAuthHandler(oauthCfg)
	handler.SetBaseURL(resolved.URL)

	// RFC 9207 §3: fetch (and cache in the handler) the AS metadata before
	// redirecting the user, so the issuer it declares can be recorded and
	// later compared against the callback's iss parameter. This also
	// primes the handler's internal metadata cache so RegisterClient and
	// GetAuthorizationURL below don't trigger a second discovery round
	// trip.
	metadata, err := handler.GetServerMetadata(ctx)
	if err != nil {
		return fmt.Errorf("mcpauth: discover authorization server metadata for %q: %w", serverName, err)
	}
	expectedIssuer := metadata.Issuer

	// mcp-go v0.57.0's AuthServerMetadata (RFC 8414) does not expose the
	// RFC 9207 §3 authorization_response_iss_parameter_supported flag, so
	// fetch that one field ourselves from the same metadata document (see
	// issuer.go for why, and the discovery-URL fallback it uses).
	httpClientForMetadata := oauthCfg.HTTPClient
	if httpClientForMetadata == nil {
		httpClientForMetadata = &http.Client{Timeout: 15 * time.Second}
	}
	issSupported := fetchIssSupport(ctx, httpClientForMetadata, oauthCfg.AuthServerMetadataURL, expectedIssuer)

	if err := m.store.UpdateMetadata(serverName, &Metadata{
		Issuer: expectedIssuer,
		AuthorizationResponseIssParameterSupported: issSupported,
	}, resolved.URL); err != nil {
		logging.Warn("mcpauth: failed to persist discovered AS metadata", "server", serverName, "error", err)
	}

	if oauthCfg.ClientID == "" {
		if err := handler.RegisterClient(ctx, clientDisplayName); err != nil {
			return fmt.Errorf("mcpauth: server %q has no client_id configured and dynamic client registration failed (%w); set Auth.OAuth.ClientID in the server config", serverName, err)
		}
	}

	state, err := transport.GenerateState()
	if err != nil {
		return fmt.Errorf("mcpauth: generate state: %w", err)
	}
	verifier, err := transport.GenerateCodeVerifier()
	if err != nil {
		return fmt.Errorf("mcpauth: generate PKCE verifier: %w", err)
	}
	challenge := transport.GenerateCodeChallenge(verifier)

	handler.SetExpectedState(state)

	authURL, err := handler.GetAuthorizationURL(ctx, state, challenge)
	if err != nil {
		return fmt.Errorf("mcpauth: build authorization URL: %w", err)
	}

	if p.OnAuthorizationURL != nil {
		p.OnAuthorizationURL(authURL)
	}
	if p.OpenBrowser {
		if err := auth.OpenBrowser(authURL); err != nil {
			logging.Warn("mcpauth: failed to open browser automatically", "error", err)
			if p.OnBrowserError != nil {
				p.OnBrowserError(err)
			}
		}
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultCallbackTimeout
	}

	var code, gotState, gotIss string
	if p.ManualCode != nil {
		code, gotState, gotIss, err = p.ManualCode(ctx)
		if err != nil {
			return fmt.Errorf("mcpauth: manual code entry failed: %w", err)
		}
		// gotIss is whatever the caller parsed out of a pasted full redirect
		// URL's `iss` query parameter (see cmd/mcp.go parseManualCodeInput);
		// it is validated below exactly like the HTTP callback path's iss.
		// A caller that only recovered a bare code (no URL to parse `iss`
		// out of) returns "", which validateIssuer treats as "nothing to
		// compare" unless the AS is known to require it.
	} else {
		code, gotIss, err = callbackSrv.Wait(ctx, state, timeout)
		gotState = state // callback server already validated state == expected
		if err != nil {
			return fmt.Errorf("mcpauth: waiting for the OAuth callback: %w", err)
		}
	}

	// A ManualCode implementation that only recovered a bare code (no state
	// visible to paste, e.g. the user typed the code from a page that
	// didn't show the full redirect URL) reports an empty gotState. Since
	// the user themselves navigated the authorization URL Pando generated
	// (embedding this exact state) and is now typing its result back by
	// hand into the same local process, there is no network attacker able
	// to inject a value here the way there is for the HTTP callback path —
	// so an empty gotState is accepted rather than treated as a mismatch.
	// Any non-empty gotState (parsed from a pasted redirect URL, or
	// returned by the HTTP callback path) must match exactly.
	if gotState != "" && gotState != state {
		return errors.New("mcpauth: authorization response state does not match the expected value (possible CSRF)")
	}

	// RFC 9207 §3: validate iss BEFORE exchanging the code. On mismatch we
	// abort here, returning our own error rather than the AS's — there is
	// no error/error_description on this path to begin with (a mismatched
	// iss only ever accompanies a successful-looking code+state response;
	// see CallbackServer.handleCallback, which resolves the AS's own
	// error responses through a separate branch entirely).
	if err := validateIssuer(expectedIssuer, gotIss, issSupported); err != nil {
		return fmt.Errorf("mcpauth: %w", err)
	}

	// handler.ProcessAuthorizationResponse validates against its own
	// expectedState (set above to `state`); always pass that value rather
	// than gotState so the empty-gotState manual-bare-code case above
	// doesn't spuriously trip the handler's own CSRF check.
	if err := handler.ProcessAuthorizationResponse(ctx, code, state, verifier); err != nil {
		if isInvalidClientOAuthError(err) && !hasStaticClientID(resolved) {
			if ierr := m.InvalidateClientRegistration(serverName); ierr != nil {
				logging.Warn("mcpauth: failed to invalidate stale client registration", "server", serverName, "error", ierr)
			}
			return fmt.Errorf("mcpauth: exchange authorization code for tokens: %w (the persisted dynamic client registration for %q looks stale or was rejected by the authorization server; it has been cleared — re-run this login to register a fresh client)", err, serverName)
		}
		return fmt.Errorf("mcpauth: exchange authorization code for tokens: %w", err)
	}

	// Persist whatever client_id/secret the handler ended up using (static
	// config or freshly registered), so a restart does not need to
	// re-register.
	if err := m.PersistClientRegistration(serverName, resolved.URL, handler); err != nil {
		logging.Warn("mcpauth: failed to persist client registration", "server", serverName, "error", err)
	}

	if !m.HasTokens(serverName, resolved.URL) {
		return fmt.Errorf("mcpauth: login for %q appeared to succeed but no tokens were persisted", serverName)
	}

	// Record the scope set this login actually used, so a future 403
	// insufficient_scope step-up (or a future Login call) accumulates on
	// top of it instead of starting from nothing.
	if err := m.RecordGrantedScopes(serverName, oauthCfg.Scopes); err != nil {
		logging.Warn("mcpauth: failed to record granted scopes", "server", serverName, "error", err)
	}

	return nil
}

// isInvalidClientOAuthError reports whether err is (or wraps) a
// transport.OAuthError with ErrorCode "invalid_client" — the standard OAuth
// 2.0 signal (RFC 6749 §5.2) that the authorization server no longer
// recognizes the client_id/client_secret presented, which for a
// dynamically-registered client typically means the registration was
// revoked, expired, or never valid at this server.
func isInvalidClientOAuthError(err error) bool {
	var oerr transport.OAuthError
	if errors.As(err, &oerr) {
		return oerr.ErrorCode == "invalid_client"
	}
	return false
}

// hasStaticClientID reports whether srv's config pins an explicit OAuth
// client_id, as opposed to relying on dynamic client registration. A
// config-pinned ClientID is never invalidated on invalid_client — that
// would just be re-read from config again on the next attempt, masking the
// real problem — so InvalidateClientRegistration is only ever triggered for
// the dynamic-registration case.
func hasStaticClientID(srv config.MCPServer) bool {
	return srv.Auth != nil && srv.Auth.OAuth != nil && srv.Auth.OAuth.ClientID != ""
}

// Status reports the current OAuth state for serverName, for `pando mcp
// status` and any future UI. srv should be the (unresolved is fine; only
// Auth.Type/OAuth.ClientID and URL are read) server config entry.
func (m *Manager) Status(serverName string, srv config.MCPServer) StatusInfo {
	info := StatusInfo{
		ServerName: serverName,
		ServerURL:  srv.URL,
		AuthType:   string(srv.Auth.ResolvedType()),
	}
	if !srv.Auth.IsOAuth() {
		return info
	}

	entry, ok, err := m.store.GetForURL(serverName, srv.URL)
	if err != nil || !ok {
		return info
	}

	if entry.Tokens != nil && entry.Tokens.AccessToken != "" {
		info.HasTokens = true
		info.ExpiresAt = entry.Tokens.ExpiresAt
		if !entry.Tokens.ExpiresAt.IsZero() && time.Now().After(entry.Tokens.ExpiresAt) {
			info.Expired = true
		}
	}
	if entry.ClientInfo != nil {
		info.ClientID = entry.ClientInfo.ClientID
		// A client ID persisted here but absent from static config was
		// necessarily obtained via dynamic client registration (Login only
		// persists ClientInfo after a successful flow, and static config
		// always takes precedence in OAuthConfig).
		if srv.Auth == nil || srv.Auth.OAuth == nil || srv.Auth.OAuth.ClientID == "" {
			info.DynamicallyRegistered = true
		}
	}
	return info
}
