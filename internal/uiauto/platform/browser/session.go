package browser

import (
	"context"
	"sync"
)

// sessionMu guards the single registered browser session slot below.
var sessionMu sync.Mutex

// sessionID / sessionCtx track the chromedp session most recently
// registered by RegisterSession. There is intentionally one slot, not a
// table keyed by pando session id: internal/uiauto.Manager (and therefore
// this backend) is a process-wide singleton, so "the" browser session is
// whichever one the browser_* agent tools most recently opened or reused.
var (
	sessionID  string
	sessionCtx context.Context
)

// RegisterSession makes an already-running chromedp session (opened by the
// browser_* agent tools, internal/llm/tools/browser_session.go, for pando
// session id) visible to the CDP accessibility backend. Call it whenever a
// browser session is created or reused. It never triggers a browser launch
// itself -- it only records a context that already exists.
func RegisterSession(id string, ctx context.Context) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessionID = id
	sessionCtx = ctx
}

// UnregisterSession clears the active session when it matches id (a no-op
// otherwise, e.g. closing a session that was already superseded by a newer
// one).
func UnregisterSession(id string) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if sessionID == id {
		sessionID = ""
		sessionCtx = nil
	}
}

// ActiveSession returns the currently registered chromedp context, if any,
// and whether it is still usable (registered and not already
// canceled/closed). It never blocks and never creates a browser.
func ActiveSession() (context.Context, bool) {
	sessionMu.Lock()
	ctx := sessionCtx
	sessionMu.Unlock()
	if ctx == nil {
		return nil, false
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return ctx, true
}
