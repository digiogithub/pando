package mcpauth

import (
	"context"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

// ServerTokenStore adapts a Store to transport.TokenStore for one named MCP
// server. mcp-go's OAuthHandler calls GetToken/SaveToken without knowing
// anything about Pando's multi-server config, so one ServerTokenStore is
// constructed per server name (see Manager.OAuthConfig).
type ServerTokenStore struct {
	store      *Store
	serverName string
	serverURL  string
}

// NewServerTokenStore returns a transport.TokenStore backed by store for the
// given server name and URL. serverURL is used both to invalidate
// credentials on URL change (see Store.GetForURL) and to stamp newly-saved
// tokens.
func NewServerTokenStore(store *Store, serverName, serverURL string) *ServerTokenStore {
	return &ServerTokenStore{store: store, serverName: serverName, serverURL: serverURL}
}

var _ transport.TokenStore = (*ServerTokenStore)(nil)

// GetToken implements transport.TokenStore.
func (s *ServerTokenStore) GetToken(ctx context.Context) (*transport.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry, ok, err := s.store.GetForURL(s.serverName, s.serverURL)
	if err != nil {
		return nil, err
	}
	if !ok || entry.Tokens == nil || entry.Tokens.AccessToken == "" {
		return nil, transport.ErrNoToken
	}
	return tokensToTransport(entry.Tokens), nil
}

// SaveToken implements transport.TokenStore.
func (s *ServerTokenStore) SaveToken(ctx context.Context, token *transport.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.store.UpdateTokens(s.serverName, transportToTokens(token), s.serverURL)
}

// tokensToTransport converts the persisted form to mcp-go's transport.Token.
func tokensToTransport(t *Tokens) *transport.Token {
	if t == nil {
		return nil
	}
	tok := &transport.Token{
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: t.RefreshToken,
		Scope:        t.Scope,
		ExpiresAt:    t.ExpiresAt,
	}
	if !t.ExpiresAt.IsZero() {
		if remaining := time.Until(t.ExpiresAt); remaining > 0 {
			tok.ExpiresIn = int64(remaining.Seconds())
		}
	}
	return tok
}

// transportToTokens converts an mcp-go transport.Token to the persisted
// form, computing ExpiresAt from ExpiresIn when the token didn't already
// carry an absolute expiry (mcp-go's own refresh path always sets
// ExpiresAt, but a hand-built token or a different OAuth library might not).
func transportToTokens(t *transport.Token) *Tokens {
	if t == nil {
		return nil
	}
	expiresAt := t.ExpiresAt
	if expiresAt.IsZero() && t.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	}
	return &Tokens{
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: t.RefreshToken,
		Scope:        t.Scope,
		ExpiresAt:    expiresAt,
	}
}
