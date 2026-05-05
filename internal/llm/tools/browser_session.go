package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	tempDir     string
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

type browserNetworkUpdate struct {
	Status   int
	MimeType string
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
	resolvedInstall, ok := ResolveBrowserInstall(cfg.BrowserType, cfg.BrowserExecutable)
	if !ok {
		return nil, fmt.Errorf("browser %q is not installed or browser executable is invalid", cfg.BrowserType)
	}
	userDataDir := strings.TrimSpace(cfg.BrowserUserDataDir)
	if userDataDir == "" {
		userDataDir = resolvedInstall.UserDataDir
	}
	profileDir := strings.TrimSpace(resolvedInstall.ProfileDir)
	ctx, ctxCancel, allocCtx, allocCancel, tempDir, err := startBrowserProcess(resolvedInstall.Executable, cfg.BrowserHeadless, userDataDir, profileDir)
	if err != nil {
		if userDataDir != "" && isBrowserProfileLockError(err) {
			fallbackCtx, fallbackCancel, fallbackAllocCtx, fallbackAllocCancel, fallbackTempDir, fallbackErr := startBrowserWithTempProfile(resolvedInstall.Executable, cfg.BrowserHeadless)
			if fallbackErr == nil {
				ctx = fallbackCtx
				ctxCancel = fallbackCancel
				allocCtx = fallbackAllocCtx
				allocCancel = fallbackAllocCancel
				tempDir = fallbackTempDir
			} else {
				return nil, fmt.Errorf("browser profile is already in use and temporary profile startup failed: %w", fallbackErr)
			}
		} else {
			return nil, fmt.Errorf("browser startup failed: %w", err)
		}
	}

	sess := &browserSession{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		ctx:         ctx,
		ctxCancel:   ctxCancel,
		lastUsed:    time.Now(),
		tempDir:     tempDir,
	}
	globalBrowserRegistry.sessions[sessionID] = sess

	// Set up event capture (console + network) for the new session.
	sess.setupConsoleCapture()
	sess.setupNetworkCapture()
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		return nil, fmt.Errorf("enable network domain: %w", err)
	}

	return sess, nil
}

func isBrowserProfileLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "process_singleton") || strings.Contains(msg, "singletonlock") || strings.Contains(msg, "singletonsocket") || strings.Contains(msg, "profile appears to be in use") || strings.Contains(msg, "user data directory is already in use")
}

func startBrowserProcess(executable string, headless bool, userDataDir, profileDir string) (context.Context, context.CancelFunc, context.Context, context.CancelFunc, string, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	if userDataDir != "" {
		opts = append(opts, chromedp.UserDataDir(userDataDir))
	}
	if profileDir != "" {
		opts = append(opts, chromedp.Flag("profile-directory", profileDir))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		ctxCancel()
		allocCancel()
		return nil, nil, nil, nil, "", err
	}
	return ctx, ctxCancel, allocCtx, allocCancel, userDataDir, nil
}

func startBrowserWithTempProfile(executable string, headless bool) (context.Context, context.CancelFunc, context.Context, context.CancelFunc, string, error) {
	tempDir, err := os.MkdirTemp("", "pando-browser-profile-*")
	if err != nil {
		return nil, nil, nil, nil, "", err
	}
	ctx, ctxCancel, allocCtx, allocCancel, _, err := startBrowserProcess(executable, headless, tempDir, "")
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, nil, nil, nil, "", err
	}
	return ctx, ctxCancel, allocCtx, allocCancel, tempDir, nil
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
	if sess.tempDir != "" {
		_ = os.RemoveAll(sess.tempDir)
	}
	delete(globalBrowserRegistry.sessions, sessionID)
}

// CloseAllBrowserSessions cancels all browser sessions. Call on app shutdown.
func CloseAllBrowserSessions() {
	globalBrowserRegistry.mu.Lock()
	defer globalBrowserRegistry.mu.Unlock()

	for id, sess := range globalBrowserRegistry.sessions {
		sess.ctxCancel()
		sess.allocCancel()
		if sess.tempDir != "" {
			_ = os.RemoveAll(sess.tempDir)
		}
		delete(globalBrowserRegistry.sessions, id)
	}
}

// setupConsoleCapture installs a chromedp event listener that captures JS console
// messages into sess.consoleLogs for later retrieval by BrowserConsoleLogsTool.
func (sess *browserSession) setupConsoleCapture() {
	chromedp.ListenTarget(sess.ctx, func(ev interface{}) {
		if msg, ok := ev.(*runtime.EventConsoleAPICalled); ok {
			var parts []string
			for _, arg := range msg.Args {
				parts = append(parts, string(arg.Value))
			}
			level := string(msg.Type)
			message := fmt.Sprintf("%s", joinStrings(parts, " "))
			entry := BrowserConsoleEntry{
				Level:   level,
				Message: message,
				Time:    time.Now(),
			}
			sess.mu.Lock()
			sess.consoleLogs = append(sess.consoleLogs, entry)
			sess.mu.Unlock()
		}
	})
}

// setupNetworkCapture installs a chromedp event listener that captures network
// requests into sess.networkLog for later retrieval by BrowserNetworkTool.
func (sess *browserSession) setupNetworkCapture() {
	updates := make(map[string]browserNetworkUpdate)

	chromedp.ListenTarget(sess.ctx, func(ev interface{}) {
		sess.mu.Lock()
		defer sess.mu.Unlock()

		switch event := ev.(type) {
		case *network.EventRequestWillBeSent:
			entry := BrowserNetworkEntry{
				RequestID: string(event.RequestID),
				URL:       event.Request.URL,
				Method:    event.Request.Method,
				Time:      time.Now(),
			}
			if update, ok := updates[entry.RequestID]; ok {
				entry.Status = update.Status
				entry.MimeType = update.MimeType
				delete(updates, entry.RequestID)
			}
			sess.networkLog = append(sess.networkLog, entry)
		case *network.EventResponseReceived:
			requestID := string(event.RequestID)
			updated := false
			for i := len(sess.networkLog) - 1; i >= 0; i-- {
				if sess.networkLog[i].RequestID == requestID {
					sess.networkLog[i].Status = int(event.Response.Status)
					sess.networkLog[i].MimeType = event.Response.MimeType
					updated = true
					break
				}
			}
			if !updated {
				updates[requestID] = browserNetworkUpdate{
					Status:   int(event.Response.Status),
					MimeType: event.Response.MimeType,
				}
			}
		case *network.EventLoadingFailed:
			requestID := string(event.RequestID)
			for i := len(sess.networkLog) - 1; i >= 0; i-- {
				if sess.networkLog[i].RequestID == requestID {
					if sess.networkLog[i].Status == 0 {
						sess.networkLog[i].MimeType = event.ErrorText
					}
					break
				}
			}
		}
	})
}

// joinStrings joins string slices — avoids importing strings just for this.
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// getBrowserCtxWithTimeout returns a chromedp context with the configured timeout
// applied, derived from the session's browser context.
// The caller must call the returned cancel func when done.
func getBrowserCtxWithTimeout(pandoCtx context.Context, overrideSeconds ...int) (context.Context, context.CancelFunc, error) {
	sessionID, _ := GetContextValues(pandoCtx)
	if sessionID == "" {
		return nil, nil, fmt.Errorf("no session ID in context")
	}

	sess, err := GetOrCreateBrowserSession(sessionID)
	if err != nil {
		return nil, nil, err
	}

	cfg := globalBrowserRegistry.cfg
	timeout := 30 * time.Second
	if cfg != nil {
		configuredTimeout := time.Duration(cfg.BrowserTimeout) * time.Second
		if configuredTimeout > 0 {
			timeout = configuredTimeout
		}
	}
	if len(overrideSeconds) > 0 && overrideSeconds[0] > 0 {
		timeout = time.Duration(overrideSeconds[0]) * time.Second
	}
	ctx, cancel := context.WithTimeout(sess.ctx, timeout)
	return ctx, cancel, nil
}
