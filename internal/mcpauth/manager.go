package mcpauth

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// DefaultCallbackPort is the local port the Phase 3 callback server listens
// on when a server's config does not set Auth.OAuth.CallbackPort.
const DefaultCallbackPort = 19876

// DefaultCallbackPath is the path component of the local OAuth redirect URI.
const DefaultCallbackPath = "/mcp/oauth/callback"

// defaultClientURI identifies this client to authorization servers that
// display it during dynamic client registration or consent.
const defaultClientURI = "https://github.com/madeindigio/pando"

// ErrAuthorizationRequired is returned (or wrapped) by Manager methods when
// a server needs an interactive OAuth authorization step that this phase
// does not perform. Phase 3 uses IsAuthorizationRequired to detect this
// condition (from this error or from mcp-go's own
// client.IsOAuthAuthorizationRequiredError) and drive the browser/callback
// flow.
var ErrAuthorizationRequired = errors.New("mcp: oauth authorization required")

// IsAuthorizationRequired reports whether err indicates that an MCP server
// needs interactive OAuth authorization, whether that error originated in
// this package (ErrAuthorizationRequired) or in mcp-go itself
// (client.IsOAuthAuthorizationRequiredError, transport.ErrOAuthAuthorizationRequired).
func IsAuthorizationRequired(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuthorizationRequired) {
		return true
	}
	if errors.Is(err, transport.ErrOAuthAuthorizationRequired) {
		return true
	}
	return client.IsOAuthAuthorizationRequiredError(err)
}

// serverState is the per-server cache entry held by Manager.
type serverState struct {
	oauthConfig transport.OAuthConfig
	tokenStore  *ServerTokenStore
	serverURL   string
	storeMTime  time.Time
}

// Manager is a process-wide, concurrency-safe cache of OAuth configuration
// built from Pando's declarative config plus the persisted Store. It does
// not perform the interactive authorization-code exchange itself (Phase 3
// owns that, driving the OAuthHandler obtained from mcp-go directly); it
// exists so that:
//   - repeated calls to acquire an MCP client for the same server reuse the
//     same TokenStore/OAuthConfig instead of re-reading disk every time,
//   - dynamically-registered client credentials survive process restarts,
//   - external changes to the credential store (another Pando process
//     completing a login, a cron rotating tokens) are picked up,
//   - concurrent 401s against the same access token trigger exactly one
//     recovery instead of a thundering herd.
type Manager struct {
	store *Store

	mu     sync.Mutex
	states map[string]*serverState

	pending sync.Map // key: serverName+"\x00"+token -> *pending401

	// stepUp caps repeated 403 insufficient_scope step-up attempts; see
	// HandleInsufficientScope in stepup.go.
	stepUp stepUpState
}

// pending401 tracks one in-flight 401-recovery so concurrent callers
// reporting the same stale access token share a single recovery attempt.
type pending401 struct {
	done chan struct{}
	err  error
}

// New401Manager-style constructors: NewManager builds a Manager over store.
// Pass mcpauth.DefaultStore() to share the default on-disk location, or a
// test-local Store (New(tempPath)) in tests.
func NewManager(store *Store) *Manager {
	if store == nil {
		store = DefaultStore()
	}
	return &Manager{store: store, states: map[string]*serverState{}}
}

var (
	defaultManagerOnce sync.Once
	defaultManager     *Manager
)

// Default returns the process-wide Manager singleton, backed by
// DefaultStore().
func Default() *Manager {
	defaultManagerOnce.Do(func() {
		defaultManager = NewManager(DefaultStore())
	})
	return defaultManager
}

// Store returns the Manager's backing Store, so Phase 3's login/logout/
// status commands and the mcpclient wiring can share exactly one on-disk
// view without importing internals.
func (m *Manager) Store() *Store {
	return m.store
}

// InvalidateIfDiskChanged compares the store's on-disk modification time
// against the last one this Manager observed for serverName and, if it has
// changed, drops the cached state so the next OAuthConfig call rebuilds it
// from the current disk contents. This lets an external process (another
// Pando instance completing `pando mcp login`, a cron rotating a secret)
// take effect without a restart.
//
// A stat failure is treated as "no change" rather than propagated, since
// missing-file is the common case (no credentials stored yet) and must not
// prevent building a fresh OAuthConfig.
func (m *Manager) InvalidateIfDiskChanged(serverName string) error {
	mtime, err := m.store.ModTime()
	if err != nil {
		logging.Debug("mcpauth: could not stat credential store, skipping invalidation check", "server", serverName, "error", err)
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[serverName]
	if !ok {
		return nil
	}
	if mtime.After(state.storeMTime) {
		delete(m.states, serverName)
	}
	return nil
}

// OAuthConfig builds (or returns the cached) transport.OAuthConfig for
// serverName using the already-decrypted srv (i.e. the result of
// config.ResolveMCPServerSecrets). Persisted ClientInfo (dynamic client
// registration results saved by PersistClientRegistration) is used when the
// static config does not set an explicit ClientID/ClientSecret; an explicit
// config value always takes precedence over what was previously persisted,
// so an operator can override a stale or misbehaving DCR registration by
// editing the config.
func (m *Manager) OAuthConfig(serverName string, srv config.MCPServer) (transport.OAuthConfig, error) {
	if err := m.InvalidateIfDiskChanged(serverName); err != nil {
		return transport.OAuthConfig{}, err
	}

	m.mu.Lock()
	if state, ok := m.states[serverName]; ok {
		cfg := state.oauthConfig
		m.mu.Unlock()
		return cfg, nil
	}
	m.mu.Unlock()

	// This builds the interactive-flow transport.OAuthConfig (redirect URI,
	// PKCE, dynamic client registration) that Login/mcpclient's interactive
	// branch use; MCPAuthOAuthClientCredentials never reaches here — see
	// clientcredentials.go's own ClientCredentialsTransport/discoverTokenEndpoint.
	if !srv.Auth.IsInteractiveOAuth() {
		return transport.OAuthConfig{}, fmt.Errorf("mcpauth: server %q auth type is not interactive oauth", serverName)
	}
	oauthCfg := srv.Auth.OAuth
	if oauthCfg == nil {
		oauthCfg = &config.MCPOAuthConfig{}
	}

	clientID := oauthCfg.ClientID
	clientSecret := oauthCfg.ClientSecret

	entry, ok, err := m.store.GetForURL(serverName, srv.URL)
	if err != nil {
		return transport.OAuthConfig{}, fmt.Errorf("mcpauth: read credential store for %q: %w", serverName, err)
	}
	if ok && entry.ClientInfo != nil && !clientInfoExpired(entry.ClientInfo) {
		if clientID == "" {
			clientID = entry.ClientInfo.ClientID
		}
		if clientSecret == "" {
			clientSecret = entry.ClientInfo.ClientSecret
		}
	} else if ok && entry.ClientInfo != nil {
		logging.Debug("mcpauth: persisted client registration secret has expired, treating it as absent so a fresh registration can run", "server", serverName)
	}

	redirectURI := strings.TrimSpace(oauthCfg.RedirectURI)
	if redirectURI == "" {
		port := oauthCfg.CallbackPort
		if port <= 0 {
			port = DefaultCallbackPort
		}
		redirectURI = "http://127.0.0.1:" + strconv.Itoa(port) + DefaultCallbackPath
	}

	tokenStore := NewServerTokenStore(m.store, serverName, srv.URL)

	cfg := transport.OAuthConfig{
		ClientID:              clientID,
		ClientURI:             defaultClientURI,
		ClientSecret:          clientSecret,
		RedirectURI:           redirectURI,
		Scopes:                append([]string(nil), oauthCfg.Scopes...),
		TokenStore:            tokenStore,
		AuthServerMetadataURL: oauthCfg.AuthServerMetadataURL,
		PKCEEnabled:           true,
	}

	mtime, err := m.store.ModTime()
	if err != nil {
		logging.Debug("mcpauth: could not stat credential store after build", "server", serverName, "error", err)
	}

	m.mu.Lock()
	m.states[serverName] = &serverState{
		oauthConfig: cfg,
		tokenStore:  tokenStore,
		serverURL:   srv.URL,
		storeMTime:  mtime,
	}
	m.mu.Unlock()

	return cfg, nil
}

// PersistClientRegistration reads the client ID/secret that h (an
// *transport.OAuthHandler obtained from an in-flight OAuth attempt, e.g.
// via client.GetOAuthHandler) ended up using — whether that came from
// static config or from a fresh RFC 7591 dynamic registration — and saves
// it to the Store under serverName/serverURL. This closes the gap where
// mcp-go performs dynamic client registration but never persists its
// result, which would otherwise force a fresh registration (and, for
// servers with a registration rate limit, failure) on every process
// restart.
//
// It also drops any cached serverState for serverName so the next
// OAuthConfig call picks up the newly-persisted ClientInfo.
func (m *Manager) PersistClientRegistration(serverName, serverURL string, h *transport.OAuthHandler) error {
	if h == nil {
		return errors.New("mcpauth: nil OAuth handler")
	}
	clientID := h.GetClientID()
	if clientID == "" {
		// Nothing to persist yet (e.g. registration hasn't happened).
		return nil
	}
	ci := &ClientInfo{ClientID: clientID, ClientSecret: h.GetClientSecret()}
	if err := m.store.UpdateClientInfo(serverName, ci, serverURL); err != nil {
		return fmt.Errorf("mcpauth: persist client registration for %q: %w", serverName, err)
	}

	m.mu.Lock()
	delete(m.states, serverName)
	m.mu.Unlock()
	return nil
}

// clientInfoExpired reports whether ci's persisted client_secret has
// expired, per the RFC 7591 §3.2.1 client_secret_expires_at convention this
// repo stores as ClientInfo.SecretExpiresAt: unix seconds, with 0 meaning
// "never expires". A ClientInfo whose secret has expired is treated as
// wholly absent (both ClientID and ClientSecret) by OAuthConfig, so a fresh
// RegisterClient runs instead of the confidential-client token exchange
// failing opaquely against an authorization server that has already
// forgotten the secret.
func clientInfoExpired(ci *ClientInfo) bool {
	if ci == nil || ci.SecretExpiresAt == 0 {
		return false
	}
	return time.Now().Unix() > ci.SecretExpiresAt
}

// InvalidateClientRegistration drops the persisted ClientInfo (dynamically
// registered client_id/client_secret) for serverName, without touching its
// tokens or requested-scope history. It is called when the authorization
// server rejects a request with the OAuth error invalid_client, which
// indicates the persisted registration is stale, revoked, or was never
// valid at this server — forcing a fresh RegisterClient on the next Login
// rather than repeating the same rejected credentials forever.
//
// Static config (an explicit Auth.OAuth.ClientID) is never invalidated this
// way; callers must only invoke this when the server's config does not pin
// a ClientID, since a config-pinned value can't be "wrong" in the same
// sense — see Login and mcpclient's invalid_client handling for the guard.
func (m *Manager) InvalidateClientRegistration(serverName string) error {
	if err := m.store.UpdateClientInfo(serverName, nil, ""); err != nil {
		return fmt.Errorf("mcpauth: invalidate client registration for %q: %w", serverName, err)
	}
	m.mu.Lock()
	delete(m.states, serverName)
	m.mu.Unlock()
	return nil
}

// Logout removes all persisted OAuth state (tokens and client
// registration) for serverName and drops any cached in-memory state.
func (m *Manager) Logout(serverName string) error {
	if err := m.store.Remove(serverName); err != nil {
		return fmt.Errorf("mcpauth: logout %q: %w", serverName, err)
	}
	m.mu.Lock()
	delete(m.states, serverName)
	m.mu.Unlock()
	return nil
}

// HasTokens reports whether a valid-looking (non-empty access token, URL
// matching) token set is persisted for serverName. It does not check
// expiry: an expired-but-refreshable token still counts, since mcp-go's
// OAuthHandler transparently refreshes it on next use.
func (m *Manager) HasTokens(serverName, serverURL string) bool {
	entry, ok, err := m.store.GetForURL(serverName, serverURL)
	if err != nil || !ok {
		return false
	}
	return entry.Tokens != nil && entry.Tokens.AccessToken != ""
}

// Do401 runs fn to recover from a 401 response that was observed with the
// given (now presumably stale) access token, ensuring that concurrent
// callers reporting a 401 for the *same* server and the *same* stale token
// share a single recovery attempt instead of each independently retrying
// (or worse, each independently kicking off an interactive re-authorization
// in Phase 3). This mirrors hermes' pending_401 deduplication.
//
// The token parameter is part of the dedup key specifically so that once
// one recovery succeeds and rotates the token, a *new* 401 (with the new,
// different stale token) starts its own recovery rather than being
// incorrectly folded into a stale in-flight one.
func (m *Manager) Do401(serverName string, token string, fn func() error) error {
	key := serverName + "\x00" + token

	p := &pending401{done: make(chan struct{})}
	actual, loaded := m.pending.LoadOrStore(key, p)
	owner := actual.(*pending401)

	if loaded {
		// Someone else is already recovering for this exact (server,
		// token) pair; wait for them instead of duplicating the work.
		<-owner.done
		return owner.err
	}

	// We are the owner: run fn, publish the result, and clean up.
	defer func() {
		close(owner.done)
		m.pending.Delete(key)
	}()
	owner.err = fn()
	return owner.err
}
