package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMCPAuthResolvedType(t *testing.T) {
	var nilAuth *MCPAuth
	if got := nilAuth.ResolvedType(); got != MCPAuthNone {
		t.Fatalf("nil auth: got %q, want %q", got, MCPAuthNone)
	}
	if got := (&MCPAuth{}).ResolvedType(); got != MCPAuthNone {
		t.Fatalf("empty type: got %q, want %q", got, MCPAuthNone)
	}
	if got := (&MCPAuth{Type: MCPAuthBearer}).ResolvedType(); got != MCPAuthBearer {
		t.Fatalf("bearer: got %q, want %q", got, MCPAuthBearer)
	}
}

func TestMCPAuthIsOAuth(t *testing.T) {
	if (&MCPAuth{Type: MCPAuthOAuth}).IsOAuth() != true {
		t.Fatal("expected IsOAuth() true for oauth type")
	}
	if (&MCPAuth{Type: MCPAuthOAuthClientCredentials}).IsOAuth() != true {
		t.Fatal("expected IsOAuth() true for oauth_client_credentials type")
	}
	if (&MCPAuth{Type: MCPAuthBearer}).IsOAuth() != false {
		t.Fatal("expected IsOAuth() false for bearer type")
	}
	var nilAuth *MCPAuth
	if nilAuth.IsOAuth() {
		t.Fatal("expected IsOAuth() false for nil auth")
	}
}

func TestMCPAuthIsInteractiveOAuth(t *testing.T) {
	if (&MCPAuth{Type: MCPAuthOAuth}).IsInteractiveOAuth() != true {
		t.Fatal("expected IsInteractiveOAuth() true for oauth type")
	}
	if (&MCPAuth{Type: MCPAuthOAuthClientCredentials}).IsInteractiveOAuth() != false {
		t.Fatal("expected IsInteractiveOAuth() false for oauth_client_credentials type")
	}
	if (&MCPAuth{Type: MCPAuthBearer}).IsInteractiveOAuth() != false {
		t.Fatal("expected IsInteractiveOAuth() false for bearer type")
	}
	var nilAuth *MCPAuth
	if nilAuth.IsInteractiveOAuth() {
		t.Fatal("expected IsInteractiveOAuth() false for nil auth")
	}
}

func TestMCPAuthValidate(t *testing.T) {
	tests := []struct {
		name    string
		auth    *MCPAuth
		wantErr bool
	}{
		{"nil", nil, false},
		{"none", &MCPAuth{Type: MCPAuthNone}, false},
		{"empty type", &MCPAuth{}, false},
		{"oauth passthrough", &MCPAuth{Type: MCPAuthOAuth}, false},
		{"bearer ok", &MCPAuth{Type: MCPAuthBearer, Token: "tok"}, false},
		{"bearer missing token", &MCPAuth{Type: MCPAuthBearer}, true},
		{"basic ok", &MCPAuth{Type: MCPAuthBasic, Username: "u", Password: "p"}, false},
		{"basic missing username", &MCPAuth{Type: MCPAuthBasic, Password: "p"}, true},
		{"header ok", &MCPAuth{Type: MCPAuthHeader, Token: "tok", HeaderName: "X-Api-Key"}, false},
		{"header missing token", &MCPAuth{Type: MCPAuthHeader, HeaderName: "X-Api-Key"}, true},
		{"unknown type", &MCPAuth{Type: "bogus"}, true},
		{"client_credentials ok", &MCPAuth{Type: MCPAuthOAuthClientCredentials, OAuth: &MCPOAuthConfig{ClientID: "id", ClientSecret: "secret"}}, false},
		{"client_credentials missing oauth block", &MCPAuth{Type: MCPAuthOAuthClientCredentials}, true},
		{"client_credentials missing client secret", &MCPAuth{Type: MCPAuthOAuthClientCredentials, OAuth: &MCPOAuthConfig{ClientID: "id"}}, true},
		{"client_credentials missing client id", &MCPAuth{Type: MCPAuthOAuthClientCredentials, OAuth: &MCPOAuthConfig{ClientSecret: "secret"}}, true},
		{"tls cert without key", &MCPAuth{Type: MCPAuthNone, ClientCert: "/tmp/cert.pem"}, true},
		{"tls key without cert", &MCPAuth{Type: MCPAuthNone, ClientKey: "/tmp/key.pem"}, true},
		{"tls cert and key ok", &MCPAuth{Type: MCPAuthNone, ClientCert: "/tmp/cert.pem", ClientKey: "/tmp/key.pem"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.auth.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMCPServerAuthHeaders(t *testing.T) {
	tests := []struct {
		name        string
		server      MCPServer
		want        map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name:   "no auth keeps explicit headers only",
			server: MCPServer{Headers: map[string]string{"X-Custom": "value"}},
			want:   map[string]string{"X-Custom": "value"},
		},
		{
			name:   "none type behaves like no auth",
			server: MCPServer{Auth: &MCPAuth{Type: MCPAuthNone}},
			want:   map[string]string{},
		},
		{
			name:   "oauth_client_credentials leaves the header to the round tripper",
			server: MCPServer{Auth: &MCPAuth{Type: MCPAuthOAuthClientCredentials, OAuth: &MCPOAuthConfig{ClientID: "id", ClientSecret: "secret"}}},
			want:   map[string]string{},
		},
		{
			name:   "bearer derives Authorization header",
			server: MCPServer{Auth: &MCPAuth{Type: MCPAuthBearer, Token: "abc123"}},
			want:   map[string]string{"Authorization": "Bearer abc123"},
		},
		{
			name: "bearer missing token errors",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthBearer},
			},
			wantErr:     true,
			errContains: "requires a token",
		},
		{
			name: "explicit Authorization header wins over bearer",
			server: MCPServer{
				Headers: map[string]string{"Authorization": "Bearer explicit"},
				Auth:    &MCPAuth{Type: MCPAuthBearer, Token: "derived"},
			},
			want: map[string]string{"Authorization": "Bearer explicit"},
		},
		{
			name: "basic derives base64 Authorization header",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthBasic, Username: "alice", Password: "wonderland"},
			},
			want: map[string]string{
				"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:wonderland")),
			},
		},
		{
			name: "basic missing username errors",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthBasic, Password: "wonderland"},
			},
			wantErr:     true,
			errContains: "requires a username",
		},
		{
			name: "header type with custom header name, no bearer prefix",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthHeader, Token: "raw-api-key", HeaderName: "X-Api-Key"},
			},
			want: map[string]string{"X-Api-Key": "raw-api-key"},
		},
		{
			name: "header type defaults to Authorization when HeaderName empty",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthHeader, Token: "raw-value"},
			},
			want: map[string]string{"Authorization": "raw-value"},
		},
		{
			name: "header missing token errors",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthHeader, HeaderName: "X-Api-Key"},
			},
			wantErr:     true,
			errContains: "requires a token",
		},
		{
			name: "oauth type produces no static header",
			server: MCPServer{
				Auth: &MCPAuth{Type: MCPAuthOAuth, OAuth: &MCPOAuthConfig{ClientID: "id"}},
			},
			want: map[string]string{},
		},
		{
			name: "unknown auth type errors",
			server: MCPServer{
				Auth: &MCPAuth{Type: "bogus"},
			},
			wantErr:     true,
			errContains: "unknown MCP auth type",
		},
		{
			name: "headers plus derived header merge",
			server: MCPServer{
				Headers: map[string]string{"X-Trace": "1"},
				Auth:    &MCPAuth{Type: MCPAuthBearer, Token: "abc123"},
			},
			want: map[string]string{"X-Trace": "1", "Authorization": "Bearer abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.server.AuthHeaders()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("AuthHeaders() unexpected error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("AuthHeaders() = %#v, want %#v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("AuthHeaders()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestMCPServerAuthHeadersDoesNotMutateSource(t *testing.T) {
	original := map[string]string{"X-Custom": "value"}
	server := MCPServer{Headers: original, Auth: &MCPAuth{Type: MCPAuthBearer, Token: "abc123"}}

	got, err := server.AuthHeaders()
	if err != nil {
		t.Fatalf("AuthHeaders() unexpected error = %v", err)
	}
	got["X-Custom"] = "mutated"
	if original["X-Custom"] != "value" {
		t.Fatal("AuthHeaders() mutated the source Headers map")
	}
}

// TestMCPAuthValidateTLSOptions covers the transport-level TLS knobs, which
// are validated independently of Type so that a typo in a corporate config is
// rejected at load time rather than at first connection.
func TestMCPAuthValidateTLSOptions(t *testing.T) {
	tests := []struct {
		name    string
		auth    MCPAuth
		wantErr string
	}{
		{name: "no tls options", auth: MCPAuth{}},
		{name: "valid version range", auth: MCPAuth{MinTLSVersion: "1.2", MaxTLSVersion: "1.3"}},
		{name: "tls prefix tolerated", auth: MCPAuth{MinTLSVersion: "TLS1.3"}},
		{name: "server name only", auth: MCPAuth{TLSServerName: "mcp.internal"}},
		{name: "ca cert with exclusive", auth: MCPAuth{CACert: "/etc/pki/ca.pem", CACertExclusive: true}},
		{
			name:    "exclusive without ca cert",
			auth:    MCPAuth{CACertExclusive: true},
			wantErr: "CACertExclusive requires CACert",
		},
		{
			name:    "deprecated tls version",
			auth:    MCPAuth{MinTLSVersion: "1.0"},
			wantErr: "unsupported TLS version",
		},
		{
			name:    "garbage max version",
			auth:    MCPAuth{MaxTLSVersion: "quic"},
			wantErr: "unsupported TLS version",
		},
		{
			name:    "min above max",
			auth:    MCPAuth{MinTLSVersion: "1.3", MaxTLSVersion: "1.2"},
			wantErr: "higher than MaxTLSVersion",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := tc.auth
			err := auth.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}
