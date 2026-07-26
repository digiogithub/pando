package mcpauth

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"

	"github.com/mark3labs/mcp-go/client/transport"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	store := New(filepath.Join(t.TempDir(), "mcp-auth.json"))
	return NewManager(store)
}

func oauthServer(url string, oauth *config.MCPOAuthConfig) config.MCPServer {
	return config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  url,
		Auth: &config.MCPAuth{Type: config.MCPAuthOAuth, OAuth: oauth},
	}
}

func TestManager_OAuthConfig_Defaults(t *testing.T) {
	m := newTestManager(t)
	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{Scopes: []string{"a", "b"}})

	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	if !cfg.PKCEEnabled {
		t.Errorf("PKCEEnabled = false, want true")
	}
	want := "http://127.0.0.1:19876/mcp/oauth/callback"
	if cfg.RedirectURI != want {
		t.Errorf("RedirectURI = %q, want %q", cfg.RedirectURI, want)
	}
	if cfg.ClientURI != defaultClientURI {
		t.Errorf("ClientURI = %q, want %q", cfg.ClientURI, defaultClientURI)
	}
	if len(cfg.Scopes) != 2 || cfg.Scopes[0] != "a" || cfg.Scopes[1] != "b" {
		t.Errorf("Scopes = %v, want [a b]", cfg.Scopes)
	}
	if cfg.TokenStore == nil {
		t.Errorf("TokenStore is nil")
	}
}

func TestManager_OAuthConfig_CustomCallbackPort(t *testing.T) {
	m := newTestManager(t)
	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{CallbackPort: 4000})

	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	want := "http://127.0.0.1:4000/mcp/oauth/callback"
	if cfg.RedirectURI != want {
		t.Errorf("RedirectURI = %q, want %q", cfg.RedirectURI, want)
	}
}

func TestManager_OAuthConfig_ConfigClientIDBeatsPersisted(t *testing.T) {
	m := newTestManager(t)
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{ClientID: "persisted-id", ClientSecret: "persisted-secret"}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{ClientID: "config-id", ClientSecret: "config-secret"})
	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	if cfg.ClientID != "config-id" {
		t.Errorf("ClientID = %q, want config-id (config beats persisted)", cfg.ClientID)
	}
	if cfg.ClientSecret != "config-secret" {
		t.Errorf("ClientSecret = %q, want config-secret", cfg.ClientSecret)
	}
}

func TestManager_OAuthConfig_PersistedUsedWhenConfigEmpty(t *testing.T) {
	m := newTestManager(t)
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{ClientID: "persisted-id", ClientSecret: "persisted-secret"}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{})
	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	if cfg.ClientID != "persisted-id" {
		t.Errorf("ClientID = %q, want persisted-id", cfg.ClientID)
	}
	if cfg.ClientSecret != "persisted-secret" {
		t.Errorf("ClientSecret = %q, want persisted-secret", cfg.ClientSecret)
	}
}

func TestManager_OAuthConfig_PersistedIgnoredOnURLChange(t *testing.T) {
	m := newTestManager(t)
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{ClientID: "persisted-id"}, "https://old.example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	srv := oauthServer("https://new.example.com/mcp", &config.MCPOAuthConfig{})
	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	if cfg.ClientID != "" {
		t.Errorf("ClientID = %q, want empty (persisted info scoped to old URL)", cfg.ClientID)
	}
}

func TestManager_OAuthConfig_RejectsNonOAuthServer(t *testing.T) {
	m := newTestManager(t)
	srv := config.MCPServer{Type: config.MCPStreamableHTTP, URL: "https://example.com"}
	if _, err := m.OAuthConfig("srv", srv); err == nil {
		t.Errorf("OAuthConfig on non-oauth server: want error, got nil")
	}
}

func TestManager_OAuthConfig_CachesResult(t *testing.T) {
	m := newTestManager(t)
	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{ClientID: "id-1"})

	cfg1, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig (1): %v", err)
	}

	// Change the config's ClientID; without invalidation, the cached value
	// should still be returned.
	srv2 := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{ClientID: "id-2"})
	cfg2, err := m.OAuthConfig("srv", srv2)
	if err != nil {
		t.Fatalf("OAuthConfig (2): %v", err)
	}
	if cfg2.ClientID != cfg1.ClientID {
		t.Errorf("expected cached OAuthConfig to be reused; got %q then %q", cfg1.ClientID, cfg2.ClientID)
	}
}

func TestManager_InvalidateIfDiskChanged(t *testing.T) {
	m := newTestManager(t)
	srv := oauthServer("https://example.com/mcp", &config.MCPOAuthConfig{})

	if _, err := m.OAuthConfig("srv", srv); err != nil {
		t.Fatalf("OAuthConfig: %v", err)
	}
	m.mu.Lock()
	if _, ok := m.states["srv"]; !ok {
		m.mu.Unlock()
		t.Fatalf("expected state to be cached")
	}
	// Force the cached mtime artificially into the past so a subsequent
	// disk write is guaranteed to look newer, regardless of filesystem
	// mtime resolution.
	m.states["srv"].storeMTime = time.Now().Add(-time.Hour)
	m.mu.Unlock()

	// Simulate an external process persisting new client info.
	if err := m.store.UpdateClientInfo("srv", &ClientInfo{ClientID: "external-id"}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateClientInfo: %v", err)
	}

	if err := m.InvalidateIfDiskChanged("srv"); err != nil {
		t.Fatalf("InvalidateIfDiskChanged: %v", err)
	}
	m.mu.Lock()
	_, stillCached := m.states["srv"]
	m.mu.Unlock()
	if stillCached {
		t.Fatalf("expected cached state to be dropped after disk change")
	}

	cfg, err := m.OAuthConfig("srv", srv)
	if err != nil {
		t.Fatalf("OAuthConfig after invalidation: %v", err)
	}
	if cfg.ClientID != "external-id" {
		t.Errorf("ClientID = %q, want external-id (picked up from disk)", cfg.ClientID)
	}
}

// fakeOAuthHandler-free test: PersistClientRegistration is exercised via a
// real *transport.OAuthHandler, since mcp-go does not expose an interface
// for it.
func TestManager_PersistClientRegistration(t *testing.T) {
	m := newTestManager(t)
	h := transport.NewOAuthHandler(transport.OAuthConfig{
		ClientID:     "dcr-client-id",
		ClientSecret: "dcr-client-secret",
	})

	if err := m.PersistClientRegistration("srv", "https://example.com/mcp", h); err != nil {
		t.Fatalf("PersistClientRegistration: %v", err)
	}

	entry, ok, err := m.store.GetForURL("srv", "https://example.com/mcp")
	if err != nil || !ok {
		t.Fatalf("GetForURL: ok=%v err=%v", ok, err)
	}
	if entry.ClientInfo == nil || entry.ClientInfo.ClientID != "dcr-client-id" {
		t.Errorf("ClientInfo = %+v, want ClientID dcr-client-id", entry.ClientInfo)
	}
	if entry.ClientInfo.ClientSecret != "dcr-client-secret" {
		t.Errorf("ClientSecret = %q, want dcr-client-secret", entry.ClientInfo.ClientSecret)
	}
}

func TestManager_PersistClientRegistration_NilHandler(t *testing.T) {
	m := newTestManager(t)
	if err := m.PersistClientRegistration("srv", "https://example.com/mcp", nil); err == nil {
		t.Errorf("PersistClientRegistration(nil): want error")
	}
}

func TestManager_LogoutAndHasTokens(t *testing.T) {
	m := newTestManager(t)
	if m.HasTokens("srv", "https://example.com/mcp") {
		t.Fatalf("HasTokens before any token: want false")
	}
	if err := m.store.UpdateTokens("srv", &Tokens{AccessToken: "at"}, "https://example.com/mcp"); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}
	if !m.HasTokens("srv", "https://example.com/mcp") {
		t.Fatalf("HasTokens after token stored: want true")
	}
	if err := m.Logout("srv"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if m.HasTokens("srv", "https://example.com/mcp") {
		t.Fatalf("HasTokens after Logout: want false")
	}
}

func TestManager_Do401_DedupesConcurrentCallers(t *testing.T) {
	m := newTestManager(t)

	var calls int32
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = m.Do401("srv", "stale-token", func() error {
				atomic.AddInt32(&calls, 1)
				time.Sleep(10 * time.Millisecond)
				return nil
			})
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("recovery fn called %d times, want exactly 1", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d got error: %v", i, err)
		}
	}
}

func TestManager_Do401_PropagatesErrorToAllWaiters(t *testing.T) {
	m := newTestManager(t)
	wantErr := errors.New("recovery failed")

	var wg sync.WaitGroup
	const n = 10
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = m.Do401("srv", "tok", func() error {
				time.Sleep(5 * time.Millisecond)
				return wantErr
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, wantErr) {
			t.Errorf("caller %d error = %v, want %v", i, err, wantErr)
		}
	}
}

func TestManager_Do401_DifferentTokensDoNotDedupe(t *testing.T) {
	m := newTestManager(t)
	var calls int32
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = m.Do401("srv", "token-a", func() error {
			atomic.AddInt32(&calls, 1)
			return nil
		})
	}()
	go func() {
		defer wg.Done()
		_ = m.Do401("srv", "token-b", func() error {
			atomic.AddInt32(&calls, 1)
			return nil
		})
	}()
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("recovery fn called %d times, want 2 (different tokens must not dedupe)", got)
	}
}

func TestIsAuthorizationRequired(t *testing.T) {
	if !IsAuthorizationRequired(ErrAuthorizationRequired) {
		t.Errorf("IsAuthorizationRequired(ErrAuthorizationRequired) = false")
	}
	if !IsAuthorizationRequired(transport.ErrOAuthAuthorizationRequired) {
		t.Errorf("IsAuthorizationRequired(transport.ErrOAuthAuthorizationRequired) = false")
	}
	if IsAuthorizationRequired(nil) {
		t.Errorf("IsAuthorizationRequired(nil) = true")
	}
	if IsAuthorizationRequired(errors.New("some other error")) {
		t.Errorf("IsAuthorizationRequired(other) = true")
	}
}

func TestDefault_ReturnsSingleton(t *testing.T) {
	if Default() != Default() {
		t.Errorf("Default() returned different instances")
	}
}
