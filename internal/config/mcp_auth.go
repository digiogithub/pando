package config

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"strings"
)

// MCPAuthType identifies the authentication mechanism used to reach an MCP
// server. Phase 1 covers the static, declarative mechanisms (none, bearer,
// basic, header); MCPAuthOAuth is declared now but its dynamic flow (RFC 9728
// discovery, DCR, PKCE, token refresh) ships in phase 2.
type MCPAuthType string

// Supported MCP authentication types.
const (
	MCPAuthNone   MCPAuthType = "none"
	MCPAuthBearer MCPAuthType = "bearer"
	MCPAuthBasic  MCPAuthType = "basic"
	MCPAuthHeader MCPAuthType = "header"
	MCPAuthOAuth  MCPAuthType = "oauth" // declared now, implemented in phase 2

	// MCPAuthOAuthClientCredentials is the OAuth 2.1 "client_credentials"
	// grant (RFC 6749 §4.4): a non-interactive, headless-friendly flow that
	// mints (and re-mints, since this grant has no refresh token) an access
	// token directly from Auth.OAuth.ClientID/ClientSecret with no browser
	// or local callback server involved. Implemented in phase 5; see
	// internal/mcpauth/clientcredentials.go.
	MCPAuthOAuthClientCredentials MCPAuthType = "oauth_client_credentials"
)

// MCPOAuthConfig holds the OAuth 2.1 client parameters for an MCP server.
// Declared in phase 1 for forward config compatibility; not yet consumed by
// the MCP client, which is phase 2 work.
type MCPOAuthConfig struct {
	ClientID     string   `json:"clientID,omitempty" toml:"ClientID" yaml:"clientID"`
	ClientSecret string   `json:"clientSecret,omitempty" toml:"ClientSecret" yaml:"clientSecret"`
	Scopes       []string `json:"scopes,omitempty" toml:"Scopes" yaml:"scopes"`
	// RedirectURI pins an explicit loopback redirect URI for the interactive
	// authorization-code flow (MCPAuthOAuth). It is a no-op for
	// MCPAuthOAuthClientCredentials: that grant (RFC 6749 §4.4) never opens a
	// browser or starts a local callback server, so there is no redirect to
	// configure.
	RedirectURI string `json:"redirectURI,omitempty" toml:"RedirectURI" yaml:"redirectURI"`
	// CallbackPort selects the port the local loopback callback server binds
	// to for the interactive authorization-code flow (MCPAuthOAuth). Like
	// RedirectURI, it is a no-op for MCPAuthOAuthClientCredentials, which has
	// no callback server at all.
	CallbackPort          int    `json:"callbackPort,omitempty" toml:"CallbackPort" yaml:"callbackPort"`
	AuthServerMetadataURL string `json:"authServerMetadataURL,omitempty" toml:"AuthServerMetadataURL" yaml:"authServerMetadataURL"`
}

// MCPAuth declares the authentication mechanism for an MCP server. A nil
// *MCPAuth (or a zero-value Type) is equivalent to MCPAuthNone: no headers are
// derived and the server behaves exactly as it did before Auth existed.
type MCPAuth struct {
	Type     MCPAuthType `json:"type,omitempty" toml:"Type" yaml:"type"`
	Token    string      `json:"token,omitempty" toml:"Token" yaml:"token"`          // bearer token / api key value
	Username string      `json:"username,omitempty" toml:"Username" yaml:"username"` // basic
	Password string      `json:"password,omitempty" toml:"Password" yaml:"password"` // basic
	// HeaderName names the header to set for MCPAuthHeader; defaults to
	// "Authorization" when empty.
	HeaderName string          `json:"headerName,omitempty" toml:"HeaderName" yaml:"headerName"`
	OAuth      *MCPOAuthConfig `json:"oauth,omitempty" toml:"OAuth" yaml:"oauth"`

	// TLS / mutual-TLS options (phase 5). All optional and independent of
	// Type: they apply to the transport-level connection (sse /
	// streamable-http) regardless of which application-level auth mechanism
	// is used, since a server can require both a client certificate and, say,
	// a bearer token. Stdio servers ignore all of these (no HTTP transport).
	//
	// ClientCert/ClientKey/CACert accept `~` and environment-variable
	// expansion (see internal/mcpauth.expandPath); ClientKeyPassword is
	// AGE-encrypted at rest like Token/Password.
	ClientCert        string `json:"clientCert,omitempty" toml:"ClientCert" yaml:"clientCert"`
	ClientKey         string `json:"clientKey,omitempty" toml:"ClientKey" yaml:"clientKey"`
	ClientKeyPassword string `json:"clientKeyPassword,omitempty" toml:"ClientKeyPassword" yaml:"clientKeyPassword"`
	CACert            string `json:"caCert,omitempty" toml:"CACert" yaml:"caCert"`
	// CACertExclusive makes CACert the ONLY trusted root for this server.
	// By default the configured CA is added to the host's system roots, since
	// an internal CA usually fronts just the MCP gateway while the
	// authorization server and other hops still chain to public roots. Set
	// this to pin the server to the private CA instead.
	CACertExclusive bool `json:"caCertExclusive,omitempty" toml:"CACertExclusive" yaml:"caCertExclusive"`
	// TLSServerName overrides the hostname used for SNI and certificate
	// verification. Use it when the MCP server is reached through an IP
	// address, an internal alias or a tunnel whose name does not match the
	// certificate — it keeps verification on, unlike SkipTLSVerify.
	TLSServerName string `json:"tlsServerName,omitempty" toml:"TLSServerName" yaml:"tlsServerName"`
	// MinTLSVersion / MaxTLSVersion pin the negotiated TLS version range,
	// written as "1.2" or "1.3" (a "TLS" prefix is tolerated). Empty means
	// the Go defaults. MinTLSVersion enforces a corporate crypto policy;
	// MaxTLSVersion exists for middleboxes that mishandle TLS 1.3.
	MinTLSVersion string `json:"minTLSVersion,omitempty" toml:"MinTLSVersion" yaml:"minTLSVersion"`
	MaxTLSVersion string `json:"maxTLSVersion,omitempty" toml:"MaxTLSVersion" yaml:"maxTLSVersion"`
	// SkipTLSVerify disables TLS certificate validation entirely. This is
	// insecure and should only be used against a known-trusted server during
	// local development/testing; internal/mcpauth.BuildTLSConfig logs a
	// warning whenever it is set.
	SkipTLSVerify bool `json:"skipTLSVerify,omitempty" toml:"SkipTLSVerify" yaml:"skipTLSVerify"`
}

// ResolvedType returns the effective auth type, treating a nil receiver or an
// empty Type as MCPAuthNone so callers never need a separate nil check.
func (a *MCPAuth) ResolvedType() MCPAuthType {
	if a == nil || a.Type == "" {
		return MCPAuthNone
	}
	return a.Type
}

// IsOAuth reports whether this auth block is configured for any OAuth
// variant — the interactive authorization-code flow (MCPAuthOAuth) or the
// non-interactive client_credentials grant (MCPAuthOAuthClientCredentials).
// Callers that need to distinguish the two (e.g. to decide whether a browser
// and local callback server are required) must use IsInteractiveOAuth
// instead.
func (a *MCPAuth) IsOAuth() bool {
	switch a.ResolvedType() {
	case MCPAuthOAuth, MCPAuthOAuthClientCredentials:
		return true
	default:
		return false
	}
}

// IsInteractiveOAuth reports whether this auth block requires the
// interactive browser + local-callback authorization-code flow driven by
// Manager.Login. It is false for MCPAuthOAuthClientCredentials, which mints
// tokens non-interactively via Manager.LoginClientCredentials and never
// opens a browser or listens for a redirect.
func (a *MCPAuth) IsInteractiveOAuth() bool {
	return a.ResolvedType() == MCPAuthOAuth
}

// defaultAuthHeaderName is the header used for bearer auth and for "header"
// auth when no explicit HeaderName is configured.
const defaultAuthHeaderName = "Authorization"

// Validate checks that the auth block is internally consistent for its
// declared Type. It does not check OAuth field completeness beyond the type
// itself, since the OAuth flow (and its own validation) belongs to phase 2.
func (a *MCPAuth) Validate() error {
	if a == nil {
		return nil
	}
	if err := a.validateTLS(); err != nil {
		return err
	}
	switch a.ResolvedType() {
	case MCPAuthNone, MCPAuthOAuth:
		return nil
	case MCPAuthOAuthClientCredentials:
		if a.OAuth == nil || strings.TrimSpace(a.OAuth.ClientID) == "" || strings.TrimSpace(a.OAuth.ClientSecret) == "" {
			return fmt.Errorf("MCP auth type %q requires Auth.OAuth.ClientID and Auth.OAuth.ClientSecret", MCPAuthOAuthClientCredentials)
		}
	case MCPAuthBearer:
		if strings.TrimSpace(a.Token) == "" {
			return fmt.Errorf("MCP auth type %q requires a token", MCPAuthBearer)
		}
	case MCPAuthBasic:
		if strings.TrimSpace(a.Username) == "" {
			return fmt.Errorf("MCP auth type %q requires a username", MCPAuthBasic)
		}
	case MCPAuthHeader:
		if strings.TrimSpace(a.Token) == "" {
			return fmt.Errorf("MCP auth type %q requires a token", MCPAuthHeader)
		}
	default:
		return fmt.Errorf("unknown MCP auth type %q", a.Type)
	}
	return nil
}

// validateTLS checks the TLS/mTLS fields that apply independently of Type:
// a client certificate requires its matching key and vice versa.
func (a *MCPAuth) validateTLS() error {
	if a == nil {
		return nil
	}
	if (strings.TrimSpace(a.ClientCert) == "") != (strings.TrimSpace(a.ClientKey) == "") {
		return fmt.Errorf("MCP auth: ClientCert and ClientKey must both be set or both be left empty")
	}
	if a.CACertExclusive && strings.TrimSpace(a.CACert) == "" {
		return fmt.Errorf("MCP auth: CACertExclusive requires CACert to be set")
	}
	minV, err := parseTLSVersionName(a.MinTLSVersion)
	if err != nil {
		return fmt.Errorf("MCP auth: MinTLSVersion: %w", err)
	}
	maxV, err := parseTLSVersionName(a.MaxTLSVersion)
	if err != nil {
		return fmt.Errorf("MCP auth: MaxTLSVersion: %w", err)
	}
	if minV != 0 && maxV != 0 && minV > maxV {
		return fmt.Errorf("MCP auth: MinTLSVersion (%s) is higher than MaxTLSVersion (%s)", a.MinTLSVersion, a.MaxTLSVersion)
	}
	return nil
}

// parseTLSVersionName validates a configured TLS version string, returning 0
// for an empty value ("use the Go default"). It intentionally duplicates the
// tiny mapping in internal/mcpauth.parseTLSVersion so that config validation
// can reject a typo at load time without internal/config depending on
// internal/mcpauth (the dependency runs the other way).
func parseTLSVersionName(v string) (uint16, error) {
	normalized := strings.ToLower(strings.TrimSpace(v))
	if normalized == "" {
		return 0, nil
	}
	normalized = strings.TrimPrefix(normalized, "tls")
	normalized = strings.TrimPrefix(normalized, "v")
	switch strings.TrimSpace(normalized) {
	case "1.2", "12":
		return tls.VersionTLS12, nil
	case "1.3", "13":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version %q (supported: \"1.2\", \"1.3\")", v)
	}
}

// AuthHeaders returns the merged set of HTTP headers for an already-decrypted
// server: s.Headers plus whatever header the static Auth block derives.
//
// Explicit entries already present in s.Headers always win over the derived
// one — a user-supplied "Authorization" header in Headers is never
// overwritten by Auth. This lets an operator override the derived header
// (e.g. to use a differently-cased key some proxy is picky about) without
// touching the Auth block.
//
// s must already be resolved via ResolveMCPServerSecrets; AuthHeaders does no
// decryption of its own.
func (s MCPServer) AuthHeaders() (map[string]string, error) {
	merged := cloneStringMap(s.Headers)
	if merged == nil {
		merged = map[string]string{}
	}

	auth := s.Auth
	switch auth.ResolvedType() {
	case MCPAuthNone, MCPAuthOAuth, MCPAuthOAuthClientCredentials:
		// OAuth (both the interactive authorization-code flow and the
		// non-interactive client_credentials grant) is handled by the
		// OAuth-aware transport wiring in internal/mcpclient using
		// internal/mcpauth, not via static headers set here. Only the
		// static headers (if any) are merged in; the Authorization header
		// itself comes from the OAuth transport's own token handling.
		return merged, nil
	case MCPAuthBearer:
		if strings.TrimSpace(auth.Token) == "" {
			return nil, fmt.Errorf("MCP auth type %q requires a token", MCPAuthBearer)
		}
		setIfAbsent(merged, defaultAuthHeaderName, "Bearer "+auth.Token)
	case MCPAuthBasic:
		if strings.TrimSpace(auth.Username) == "" {
			return nil, fmt.Errorf("MCP auth type %q requires a username", MCPAuthBasic)
		}
		creds := auth.Username + ":" + auth.Password
		encoded := base64.StdEncoding.EncodeToString([]byte(creds))
		setIfAbsent(merged, defaultAuthHeaderName, "Basic "+encoded)
	case MCPAuthHeader:
		if strings.TrimSpace(auth.Token) == "" {
			return nil, fmt.Errorf("MCP auth type %q requires a token", MCPAuthHeader)
		}
		headerName := strings.TrimSpace(auth.HeaderName)
		if headerName == "" {
			headerName = defaultAuthHeaderName
		}
		// No "Bearer " prefix here: the user supplies the full header value.
		setIfAbsent(merged, headerName, auth.Token)
	default:
		return nil, fmt.Errorf("unknown MCP auth type %q", auth.Type)
	}
	return merged, nil
}

// cloneMCPAuth returns a deep copy of a, or nil if a is nil. It is used
// wherever an MCPServer is copied so that mutating the clone's Auth (or its
// nested OAuth config) never reaches back into the original.
func cloneMCPAuth(a *MCPAuth) *MCPAuth {
	if a == nil {
		return nil
	}
	clone := *a
	if a.OAuth != nil {
		oauth := *a.OAuth
		oauth.Scopes = append([]string(nil), a.OAuth.Scopes...)
		clone.OAuth = &oauth
	}
	return &clone
}

// setIfAbsent sets m[key] = value unless m already has a (case-sensitive) key
// with that exact name. HTTP header maps in this codebase are plain
// map[string]string with no canonicalization, so an explicit Headers entry
// using a different case than the derived key would result in two headers
// being sent; that mirrors the pre-existing behavior of Headers-only config
// and is out of scope for phase 1.
func setIfAbsent(m map[string]string, key, value string) {
	if _, ok := m[key]; ok {
		return
	}
	m[key] = value
}
