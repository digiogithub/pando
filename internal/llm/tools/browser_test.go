package tools

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func TestBrowserRegistryInit(t *testing.T) {
	cfg := &config.InternalToolsConfig{
		BrowserEnabled:     true,
		BrowserHeadless:    true,
		BrowserTimeout:     10,
		BrowserMaxSessions: 2,
	}
	InitBrowserRegistry(cfg)

	globalBrowserRegistry.mu.Lock()
	got := globalBrowserRegistry.cfg
	globalBrowserRegistry.mu.Unlock()

	if got == nil {
		t.Fatal("expected cfg to be set after InitBrowserRegistry")
	}
	if got.BrowserMaxSessions != 2 {
		t.Errorf("expected BrowserMaxSessions=2, got %d", got.BrowserMaxSessions)
	}
}

func TestBrowserSessionCleanup(t *testing.T) {
	cfg := &config.InternalToolsConfig{
		BrowserEnabled:     true,
		BrowserHeadless:    true,
		BrowserTimeout:     10,
		BrowserMaxSessions: 3,
	}
	InitBrowserRegistry(cfg)

	sessionID := "test-cleanup-session"
	_, err := GetOrCreateBrowserSession(sessionID)
	if err != nil {
		t.Skipf("Chrome not available: %v", err)
	}

	CloseBrowserSession(sessionID)

	globalBrowserRegistry.mu.Lock()
	_, exists := globalBrowserRegistry.sessions[sessionID]
	globalBrowserRegistry.mu.Unlock()

	if exists {
		t.Error("session should be removed after CloseBrowserSession")
	}
}

func TestBrowserMaxSessions(t *testing.T) {
	cfg := &config.InternalToolsConfig{
		BrowserEnabled:     true,
		BrowserHeadless:    true,
		BrowserTimeout:     10,
		BrowserMaxSessions: 1,
	}
	InitBrowserRegistry(cfg)
	// Reset sessions to ensure clean state
	CloseAllBrowserSessions()

	_, err := GetOrCreateBrowserSession("session-a")
	if err != nil {
		t.Skipf("Chrome not available: %v", err)
	}
	defer CloseBrowserSession("session-a")

	_, err = GetOrCreateBrowserSession("session-b")
	if err == nil {
		CloseBrowserSession("session-b")
		t.Error("expected error when max sessions reached, got nil")
	}
}

func TestCloseAllBrowserSessions(t *testing.T) {
	cfg := &config.InternalToolsConfig{
		BrowserEnabled:     true,
		BrowserHeadless:    true,
		BrowserTimeout:     10,
		BrowserMaxSessions: 3,
	}
	InitBrowserRegistry(cfg)

	for _, id := range []string{"sess-1", "sess-2"} {
		_, err := GetOrCreateBrowserSession(id)
		if err != nil {
			t.Skipf("Chrome not available: %v", err)
		}
	}

	CloseAllBrowserSessions()

	globalBrowserRegistry.mu.Lock()
	count := len(globalBrowserRegistry.sessions)
	globalBrowserRegistry.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 sessions after CloseAllBrowserSessions, got %d", count)
	}
}
