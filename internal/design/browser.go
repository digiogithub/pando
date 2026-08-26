package design

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/digiogithub/pando/internal/browser"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// ErrNoBrowser is returned when no Chromium-family browser can be resolved.
// Callers degrade gracefully: the live preview is the user's own browser, so a
// missing Chromium only costs screenshots, inspection, export and rasterizing.
var ErrNoBrowser = fmt.Errorf("design: no browser available")

// ConsoleEntry is a JavaScript console message captured during a render.
type ConsoleEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// NetworkFailure is a request that failed or answered with an error status
// while rendering. Broken assets are a design defect, so they are reported to
// the agent with the render result instead of being swallowed.
type NetworkFailure struct {
	URL    string `json:"url"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// BrowserOptions configures how the design renderer launches a browser.
type BrowserOptions struct {
	// Type is a browser type understood by internal/browser ("chrome",
	// "chromium", "msedge", "brave", "lightpanda", …). Empty means auto-detect.
	Type string
	// Executable overrides the resolved browser binary.
	Executable string
	// Headless runs the browser without a window. Default: true.
	Headless bool
	// IdleTimeout closes the browser after this long without a render.
	// Zero means the session stays open until Close.
	IdleTimeout time.Duration
}

// BrowserOptionsFromConfig reads the browser settings the browser_* tools
// already use, so Pando drives one browser configuration, not two.
func BrowserOptionsFromConfig() BrowserOptions {
	it := config.Get().InternalTools
	return BrowserOptions{
		Type:        it.BrowserType,
		Executable:  it.BrowserExecutable,
		Headless:    true,
		IdleTimeout: 5 * time.Minute,
	}
}

// browserSession owns one chromedp browser used for design renders. It is
// separate from the browser_* tool session on purpose: rendering an artifact
// must not navigate away from the page the agent is interacting with, and it
// always runs headless in a throwaway profile.
type browserSession struct {
	mu sync.Mutex

	opts BrowserOptions

	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	ctxCancel   context.CancelFunc
	tempDir     string
	lastUsed    time.Time

	// Event buffers, reset at the start of every render.
	eventsMu sync.Mutex
	console  []ConsoleEntry
	failures []NetworkFailure
	// requests maps an in-flight request id to its URL. A loading failure
	// carries only the id, and a finding that cannot name the file it could not
	// load is a finding nobody can act on.
	requests map[network.RequestID]string
}

func newBrowserSession(opts BrowserOptions) *browserSession {
	return &browserSession{opts: opts}
}

// context returns a live browser context, starting the browser on first use.
// The caller must hold s.mu.
func (s *browserSession) context() (context.Context, error) {
	if s.ctx != nil && s.ctx.Err() == nil {
		s.lastUsed = time.Now()
		return s.ctx, nil
	}
	s.shutdown()

	install, ok := browser.ResolveBrowserInstall(s.opts.Type, s.opts.Executable)
	if !ok {
		return nil, fmt.Errorf("%w: configure internalTools.browserType or install Chrome/Chromium", ErrNoBrowser)
	}
	if browser.IsRemoteBrowserType(install.Type) {
		// Lightpanda and other CDP-server browsers do not implement the
		// screenshot/print surface the design renderer depends on.
		return nil, fmt.Errorf("%w: %s cannot render design artifacts (screenshots and PDF need Chromium)",
			ErrNoBrowser, install.Label)
	}

	tempDir, err := os.MkdirTemp("", "pando-design-profile-*")
	if err != nil {
		return nil, fmt.Errorf("design: create browser profile dir: %w", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(install.Executable),
		chromedp.Flag("headless", s.opts.Headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", true),
		// The renderer opens file:// URLs from the artifact directory; it never
		// needs cross-origin access, and the sandboxing rules of the preview
		// server (P3) must not be weakened here.
		chromedp.UserDataDir(tempDir),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		ctxCancel()
		allocCancel()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("design: start browser %s: %w", install.Executable, err)
	}

	s.allocCtx, s.allocCancel = allocCtx, allocCancel
	s.ctx, s.ctxCancel = ctx, ctxCancel
	s.tempDir = tempDir
	s.lastUsed = time.Now()

	s.listen(ctx)
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		logging.Warn("design: enable network domain", "error", err)
	}

	logging.Debug("design: browser started", "executable", install.Executable)
	return ctx, nil
}

// listen wires console and network capture for the session.
func (s *browserSession) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			msg := ""
			for i, arg := range e.Args {
				if i > 0 {
					msg += " "
				}
				if len(arg.Value) > 0 {
					msg += trimQuotes(string(arg.Value))
					continue
				}
				msg += arg.Description
			}
			s.record(ConsoleEntry{Level: string(e.Type), Message: msg})
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails != nil {
				s.record(ConsoleEntry{Level: "exception", Message: e.ExceptionDetails.Error()})
			}
		case *network.EventRequestWillBeSent:
			if e.Request != nil {
				s.rememberRequest(e.RequestID, e.Request.URL)
			}
		case *network.EventLoadingFailed:
			s.recordFailure(NetworkFailure{URL: s.forgetRequest(e.RequestID), Error: e.ErrorText})
		case *network.EventLoadingFinished:
			s.forgetRequest(e.RequestID)
		case *network.EventResponseReceived:
			if e.Response != nil && e.Response.Status >= 400 {
				s.recordFailure(NetworkFailure{URL: e.Response.URL, Status: int(e.Response.Status)})
			}
		}
	})
}

func (s *browserSession) record(entry ConsoleEntry) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	if len(s.console) < maxCapturedEvents {
		s.console = append(s.console, entry)
	}
}

// rememberRequest and forgetRequest keep the in-flight map bounded: an entry
// lives only until the request finishes or fails.
func (s *browserSession) rememberRequest(id network.RequestID, url string) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	if s.requests == nil {
		s.requests = make(map[network.RequestID]string)
	}
	if len(s.requests) < maxTrackedRequests {
		s.requests[id] = url
	}
}

func (s *browserSession) forgetRequest(id network.RequestID) string {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	url := s.requests[id]
	delete(s.requests, id)
	return url
}

func (s *browserSession) recordFailure(f NetworkFailure) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	if len(s.failures) < maxCapturedEvents {
		s.failures = append(s.failures, f)
	}
}

// maxCapturedEvents bounds what a runaway page can push into a render result.
const maxCapturedEvents = 100

// maxTrackedRequests bounds the in-flight request map for the same reason.
const maxTrackedRequests = 500

// resetEvents clears the buffers before a navigation.
func (s *browserSession) resetEvents() {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	s.console = nil
	s.failures = nil
	s.requests = nil
}

// takeEvents returns and clears what the last render captured.
func (s *browserSession) takeEvents() ([]ConsoleEntry, []NetworkFailure) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	console, failures := s.console, s.failures
	s.console, s.failures = nil, nil
	return console, failures
}

// Close releases the browser and its temporary profile.
func (s *browserSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdown()
}

// shutdown tears the browser down. The caller must hold s.mu.
func (s *browserSession) shutdown() {
	if s.ctxCancel != nil {
		s.ctxCancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
	if s.tempDir != "" {
		_ = os.RemoveAll(s.tempDir)
	}
	s.ctx, s.ctxCancel = nil, nil
	s.allocCtx, s.allocCancel = nil, nil
	s.tempDir = ""
}

// expireIfIdle closes the browser when it has not been used for IdleTimeout.
func (s *browserSession) expireIfIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opts.IdleTimeout <= 0 || s.ctx == nil {
		return
	}
	if time.Since(s.lastUsed) > s.opts.IdleTimeout {
		logging.Debug("design: closing idle browser")
		s.shutdown()
	}
}

// trimQuotes strips the quotes JSON string values arrive wrapped in.
func trimQuotes(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}
