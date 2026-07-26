package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"

	"github.com/mark3labs/mcp-go/client/transport"
)

// clientCredentialsExpiryLeeway is subtracted from a token's recorded
// ExpiresAt when deciding whether it is still usable, so a token that would
// expire mid-flight on a slow request is proactively re-minted instead.
const clientCredentialsExpiryLeeway = 30 * time.Second

// maxClientCredentialsResponseBytes caps how much of a token endpoint's
// response body is read.
const maxClientCredentialsResponseBytes = 1 << 20 // 1 MiB

// discoveryHTTPTimeout bounds the metadata-discovery and token-mint requests
// this file makes; there is no long-lived streaming involved.
const discoveryHTTPTimeout = 15 * time.Second

// ClientCredentialsParams holds everything needed to mint (or re-mint) an
// OAuth 2.1 client_credentials access token (RFC 6749 §4.4) for one MCP
// server.
type ClientCredentialsParams struct {
	// TokenEndpoint is the authorization server's token endpoint, normally
	// discovered via discoverTokenEndpoint (RFC 8414 metadata).
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	Scopes        []string
	// Resource is the RFC 8707 canonical resource-server URI to send as the
	// "resource" form parameter, or empty to omit it.
	Resource string
	// HTTPClient is used for the token request. A nil value falls back to a
	// plain client with discoveryHTTPTimeout.
	HTTPClient *http.Client
}

// mintClientCredentialsToken performs one OAuth 2.1 client_credentials grant
// (RFC 6749 §4.4) using client_secret_post (client_id/client_secret in the
// form body, the simplest and most broadly supported confidential-client
// authentication method) plus the optional scope and RFC 8707 resource
// parameters, and returns the resulting token set.
func mintClientCredentialsToken(ctx context.Context, p ClientCredentialsParams) (*Tokens, error) {
	if strings.TrimSpace(p.TokenEndpoint) == "" {
		return nil, errors.New("mcpauth: no token endpoint discovered for the client_credentials grant")
	}
	if strings.TrimSpace(p.ClientID) == "" || strings.TrimSpace(p.ClientSecret) == "" {
		return nil, errors.New("mcpauth: client_credentials grant requires ClientID and ClientSecret")
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", p.ClientID)
	data.Set("client_secret", p.ClientSecret)
	if len(p.Scopes) > 0 {
		data.Set("scope", strings.Join(p.Scopes, " "))
	}
	if strings.TrimSpace(p.Resource) != "" {
		data.Set("resource", p.Resource)
	}

	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: discoveryHTTPTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("mcpauth: build client_credentials token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: send client_credentials token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxClientCredentialsResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("mcpauth: read client_credentials token response: %w", err)
	}

	// Accept any 2xx; some authorization servers return 201 for a
	// successful token response (mirrors mcp-go's own refreshToken).
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, extractClientCredentialsError(body, resp.StatusCode)
	}

	// Some authorization servers (mirroring the GitHub quirk mcp-go itself
	// works around) return HTTP 200 with error details in the JSON body.
	var oauthErr transport.OAuthError
	if err := json.Unmarshal(body, &oauthErr); err == nil && oauthErr.ErrorCode != "" {
		return nil, fmt.Errorf("mcpauth: client_credentials token request failed: %w", oauthErr)
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("mcpauth: decode client_credentials token response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return nil, errors.New("mcpauth: client_credentials token endpoint response has no access_token")
	}

	tok := &Tokens{
		AccessToken: tr.AccessToken,
		TokenType:   tr.TokenType,
		Scope:       tr.Scope,
	}
	if tr.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return tok, nil
}

// extractClientCredentialsError mirrors mcp-go's own extractOAuthError:
// prefer a parsed RFC 6749 §5.2 error object, falling back to the raw body.
func extractClientCredentialsError(body []byte, statusCode int) error {
	var oe transport.OAuthError
	if err := json.Unmarshal(body, &oe); err == nil && oe.ErrorCode != "" {
		return fmt.Errorf("mcpauth: client_credentials token request failed: %w", oe)
	}
	return fmt.Errorf("mcpauth: client_credentials token request failed with status %d: %s", statusCode, string(body))
}

// CanonicalResourceURI implements the RFC 8707 canonical resource-server URI
// this package uses as the "resource" parameter on client_credentials token
// requests: lowercase scheme and host, default ports (80 for http, 443 for
// https) stripped, fragment dropped, trailing slash on a non-root path
// dropped, path otherwise preserved as-is. The query string is dropped too
// (a resource identifier is scheme+host+path, not a request URI).
func CanonicalResourceURI(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("mcpauth: parse resource URI %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("mcpauth: resource URI %q is not an absolute URL", raw)
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	hostport := host
	if port != "" {
		hostport = host + ":" + port
	}

	path := u.EscapedPath()
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}

	return scheme + "://" + hostport + path, nil
}

// discoverTokenEndpoint discovers the authorization server's token endpoint
// for serverName via RFC 8414 metadata discovery, reusing mcp-go's own
// OAuthHandler.GetServerMetadata rather than re-implementing discovery.
func (m *Manager) discoverTokenEndpoint(ctx context.Context, serverName string, srv config.MCPServer, httpClient *http.Client) (string, error) {
	metadataURL := ""
	if srv.Auth != nil && srv.Auth.OAuth != nil {
		metadataURL = srv.Auth.OAuth.AuthServerMetadataURL
	}
	handler := transport.NewOAuthHandler(transport.OAuthConfig{
		AuthServerMetadataURL: metadataURL,
		HTTPClient:            httpClient,
	})
	handler.SetBaseURL(srv.URL)

	metadata, err := handler.GetServerMetadata(ctx)
	if err != nil {
		return "", fmt.Errorf("mcpauth: discover authorization server metadata for %q: %w", serverName, err)
	}
	if strings.TrimSpace(metadata.TokenEndpoint) == "" {
		return "", fmt.Errorf("mcpauth: authorization server metadata for %q has no token_endpoint", serverName)
	}
	return metadata.TokenEndpoint, nil
}

// LoginClientCredentials performs the non-interactive OAuth 2.1
// client_credentials grant for serverName (Auth.Type ==
// MCPAuthOAuthClientCredentials): discovers the token endpoint, mints an
// access token, and persists it through the Store — no browser, no local
// callback server, suitable for headless/CI use from `pando mcp login`.
func (m *Manager) LoginClientCredentials(ctx context.Context, serverName string, srv config.MCPServer) error {
	if srv.Type == config.MCPStdio {
		return fmt.Errorf("mcpauth: server %q is a stdio server; stdio servers take credentials from the environment, not OAuth", serverName)
	}
	resolved, err := config.ResolveMCPServerSecrets(srv)
	if err != nil {
		return fmt.Errorf("mcpauth: resolve server %q secrets: %w", serverName, err)
	}
	if resolved.Auth.ResolvedType() != config.MCPAuthOAuthClientCredentials {
		return fmt.Errorf("mcpauth: server %q auth type is %q, not %q", serverName, resolved.Auth.ResolvedType(), config.MCPAuthOAuthClientCredentials)
	}
	if resolved.Auth.OAuth == nil || strings.TrimSpace(resolved.Auth.OAuth.ClientID) == "" || strings.TrimSpace(resolved.Auth.OAuth.ClientSecret) == "" {
		return fmt.Errorf("mcpauth: server %q client_credentials auth requires Auth.OAuth.ClientID and Auth.OAuth.ClientSecret", serverName)
	}
	if strings.TrimSpace(resolved.URL) == "" {
		return fmt.Errorf("mcpauth: server %q has no URL configured", serverName)
	}

	tlsCfg, err := BuildTLSConfig(resolved)
	if err != nil {
		return fmt.Errorf("mcpauth: %w", err)
	}
	httpClient := HTTPClientForTLS(tlsCfg, discoveryHTTPTimeout)

	tokenEndpoint, err := m.discoverTokenEndpoint(ctx, serverName, resolved, httpClient)
	if err != nil {
		return err
	}

	resource, err := CanonicalResourceURI(resolved.URL)
	if err != nil {
		logging.Warn("mcpauth: could not canonicalize resource URI, omitting RFC 8707 resource parameter", "server", serverName, "error", err)
		resource = ""
	}

	tok, err := mintClientCredentialsToken(ctx, ClientCredentialsParams{
		TokenEndpoint: tokenEndpoint,
		ClientID:      resolved.Auth.OAuth.ClientID,
		ClientSecret:  resolved.Auth.OAuth.ClientSecret,
		Scopes:        resolved.Auth.OAuth.Scopes,
		Resource:      resource,
		HTTPClient:    httpClient,
	})
	if err != nil {
		return fmt.Errorf("mcpauth: client_credentials login for %q: %w", serverName, err)
	}

	if err := m.store.UpdateTokens(serverName, tok, resolved.URL); err != nil {
		return fmt.Errorf("mcpauth: persist client_credentials token for %q: %w", serverName, err)
	}

	m.mu.Lock()
	delete(m.states, serverName)
	m.mu.Unlock()
	return nil
}

// clientCredentialsTokenSource mints and refreshes a client_credentials
// access token on demand, caching it through Store so the same token is
// reused across calls (and across processes) until it is close to expiry.
// This grant has no refresh token (RFC 6749 §4.4), so "refreshing" always
// means requesting a brand new token with the same client credentials.
type clientCredentialsTokenSource struct {
	store      *Store
	serverName string
	serverURL  string
	params     ClientCredentialsParams

	mu sync.Mutex
}

// AccessToken returns a currently-valid access token, minting a fresh one
// (and persisting it) if the cached one is missing or close to expiry.
func (ts *clientCredentialsTokenSource) AccessToken(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	entry, ok, err := ts.store.GetForURL(ts.serverName, ts.serverURL)
	if err == nil && ok && entry.Tokens != nil && entry.Tokens.AccessToken != "" {
		if entry.Tokens.ExpiresAt.IsZero() || time.Now().Before(entry.Tokens.ExpiresAt.Add(-clientCredentialsExpiryLeeway)) {
			return entry.Tokens.AccessToken, nil
		}
	}

	tok, err := mintClientCredentialsToken(ctx, ts.params)
	if err != nil {
		return "", err
	}
	if err := ts.store.UpdateTokens(ts.serverName, tok, ts.serverURL); err != nil {
		logging.Warn("mcpauth: failed to persist re-minted client_credentials token", "server", ts.serverName, "error", err)
	}
	return tok.AccessToken, nil
}

// bearerRoundTripper attaches "Authorization: Bearer <token>" to every
// request, minting/refreshing the token on demand via source, then
// delegates to base (http.DefaultTransport if nil).
//
// This — rather than mcp-go's OAuthConfig/OAuthHandler machinery used for
// the interactive flow — is the integration point for
// MCPAuthOAuthClientCredentials: OAuthHandler.getValidToken only ever
// refreshes via a stored refresh_token or otherwise returns
// ErrOAuthAuthorizationRequired, which would make a client_credentials
// token expiring mid-session unrecoverable without a spurious "please log in
// interactively" error. A plain http.RoundTripper wrapping a small token
// source is the simplest correct design that avoids that dead end while
// still reusing NewSSEMCPClient/NewStreamableHttpClient (not their
// OAuth-specific counterparts) plus internal/mcpauth's own token minting and
// Store persistence.
type bearerRoundTripper struct {
	base   http.RoundTripper
	source *clientCredentialsTokenSource
}

var _ http.RoundTripper = (*bearerRoundTripper)(nil)

func (t *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.source.AccessToken(req.Context())
	if err != nil {
		return nil, fmt.Errorf("mcpauth: obtain client_credentials access token: %w", err)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+token)
	return base.RoundTrip(cloned)
}

// ClientCredentialsTransport builds the http.RoundTripper internal/mcpclient
// wires into the plain (non-OAuth-handler) SSE/streamable-http transports
// for a server whose Auth.Type is MCPAuthOAuthClientCredentials: it
// discovers the token endpoint, then returns a bearerRoundTripper that mints
// and caches tokens through the Store on demand, wrapping base (the
// TLS-configured transport, or nil for the library default).
//
// resolved must already have its secrets resolved via
// config.ResolveMCPServerSecrets.
func (m *Manager) ClientCredentialsTransport(ctx context.Context, serverName string, resolved config.MCPServer, base http.RoundTripper) (http.RoundTripper, error) {
	if resolved.Auth.ResolvedType() != config.MCPAuthOAuthClientCredentials {
		return nil, fmt.Errorf("mcpauth: server %q auth type is %q, not %q", serverName, resolved.Auth.ResolvedType(), config.MCPAuthOAuthClientCredentials)
	}
	if resolved.Auth.OAuth == nil || strings.TrimSpace(resolved.Auth.OAuth.ClientID) == "" || strings.TrimSpace(resolved.Auth.OAuth.ClientSecret) == "" {
		return nil, fmt.Errorf("mcpauth: server %q client_credentials auth requires Auth.OAuth.ClientID and Auth.OAuth.ClientSecret", serverName)
	}

	discoveryClient := HTTPClientForTLS(nil, discoveryHTTPTimeout)
	if base != nil {
		discoveryClient = &http.Client{Transport: base, Timeout: discoveryHTTPTimeout}
	}

	tokenEndpoint, err := m.discoverTokenEndpoint(ctx, serverName, resolved, discoveryClient)
	if err != nil {
		return nil, err
	}

	resource, err := CanonicalResourceURI(resolved.URL)
	if err != nil {
		logging.Warn("mcpauth: could not canonicalize resource URI, omitting RFC 8707 resource parameter", "server", serverName, "error", err)
		resource = ""
	}

	params := ClientCredentialsParams{
		TokenEndpoint: tokenEndpoint,
		ClientID:      resolved.Auth.OAuth.ClientID,
		ClientSecret:  resolved.Auth.OAuth.ClientSecret,
		Scopes:        resolved.Auth.OAuth.Scopes,
		Resource:      resource,
		HTTPClient:    discoveryClient,
	}
	source := &clientCredentialsTokenSource{store: m.store, serverName: serverName, serverURL: resolved.URL, params: params}
	return &bearerRoundTripper{base: base, source: source}, nil
}
