package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/digiogithub/pando/internal/config"
)

// browserSession holds a chromedp allocator + browser context for one pando session.
type browserSession struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	ctxCancel   context.CancelFunc
	lastUsed    time.Time
	// Console and network event buffers (used by devtools tools)
	consoleLogs []BrowserConsoleEntry
	networkLog  []BrowserNetworkEntry
	mu          sync.Mutex
}

// BrowserConsoleEntry holds a single JS console message captured during a browser session.
type BrowserConsoleEntry struct {
	Level   string
	Message string
	Time    time.Time
}

// BrowserNetworkEntry holds metadata for a single network request captured during a browser session.
type BrowserNetworkEntry struct {
	RequestID string
	URL       string
	Method    string
	Status    int
	MimeType  string
	Time      time.Time
}

type browserRegistry struct {
	mu       sync.Mutex
	sessions map[string]*browserSession
	cfg      *config.InternalToolsConfig
}

var globalBrowserRegistry = &browserRegistry{
	sessions: make(map[string]*browserSession),
}

// InitBrowserRegistry sets the config for the global browser registry.
// Must be called at app startup when BrowserEnabled=true.
func InitBrowserRegistry(cfg *config.InternalToolsConfig) {
	globalBrowserRegistry.mu.Lock()
	defer globalBrowserRegistry.mu.Unlock()
	globalBrowserRegistry.cfg = cfg
}

// GetOrCreateBrowserSession returns the existing browser session for the given
// pando sessionID, or creates a new one if it doesn't exist.
func GetOrCreateBrowserSession(sessionID string) (*browserSession, error) {
	globalBrowserRegistry.mu.Lock()
	defer globalBrowserRegistry.mu.Unlock()

	if sess, ok := globalBrowserRegistry.sessions[sessionID]; ok {
		sess.lastUsed = time.Now()
		return sess, nil
	}

	cfg := globalBrowserRegistry.cfg
	if cfg == nil {
		return nil, fmt.Errorf("browser registry not initialized")
	}

	// Enforce max sessions limit
	if len(globalBrowserRegistry.sessions) >= cfg.BrowserMaxSessions {
		return nil, fmt.Errorf("max browser sessions (%d) reached; close existing sessions first", cfg.BrowserMaxSessions)
	}

	// Build allocator options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", cfg.BrowserHeadless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	if cfg.BrowserUserDataDir != "" {
		opts = append(opts, chromedp.UserDataDir(cfg.BrowserUserDataDir))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)

	sess := &browserSession{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		ctx:         ctx,
		ctxCancel:   ctxCancel,
		lastUsed:    time.Now(),
	}
	globalBrowserRegistry.sessions[sessionID] = sess

	// Set up event capture (console + network) for the new session.
	sess.setupConsoleCapture()
	if err := chromedp.Run(ctx, network.Enable()); err == nil {
		sess.setupNetworkCapture()
	}

	return sess, nil
}

// CloseBrowserSession cancels and removes the browser session for the given pando sessionID.
func CloseBrowserSession(sessionID string) {
	globalBrowserRegistry.mu.Lock()
	defer globalBrowserRegistry.mu.Unlock()

	sess, ok := globalBrowserRegistry.sessions[sessionID]
	if !ok {
		return
	}
	sess.ctxCancel()
	sess.allocCancel()
	delete(globalBrowserRegistry.sessions, sessionID)
}

// CloseAllBrowserSessions cancels all browser sessions. Call on app shutdown.
func CloseAllBrowserSessions() {
	globalBrowserRegistry.mu.Lock()
	defer globalBrowserRegistry.mu.Unlock()

	for id, sess := range globalBrowserRegistry.sessions {
		sess.ctxCancel()
		sess.allocCancel()
		delete(globalBrowserRegistry.sessions, id)
	}
}

// getBrowserCtxWithTimeout returns a chromedp context with the configured timeout
// applied, derived from the session's browser context.
// The caller must call the returned cancel func when done.
func getBrowserCtxWithTimeout(pandoCtx context.Context) (context.Context, context.CancelFunc, error) {
	sessionID, _ := GetContextValues(pandoCtx)
	if sessionID == "" {
		return nil, nil, fmt.Errorf("no session ID in context")
	}

	sess, err := GetOrCreateBrowserSession(sessionID)
	if err != nil {
		return nil, nil, err
	}

	cfg := globalBrowserRegistry.cfg
	timeout := time.Duration(cfg.BrowserTimeout) * time.Second
	ctx, cancel := context.WithTimeout(sess.ctx, timeout)
	return ctx, cancel, nil
}
