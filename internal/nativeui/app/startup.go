package app

import "sync"

var (
	startMu    sync.Mutex
	startHooks []func(a *Context)
)

// OnStart registers fn to run once, on the UI thread, in the first frame of
// the root view (after the dispatcher is connected). Areas use it to react to
// the server state at start-up, e.g. the setup area opens its assistant when
// the server says the first-run setup is needed. fn must not block: start
// goroutines with a.Go and come back through a.Update.
func OnStart(fn func(a *Context)) {
	startMu.Lock()
	startHooks = append(startHooks, fn)
	startMu.Unlock()
}

// runStartHooks runs the registered hooks (once per Root).
func (r *Root) runStartHooks() {
	if r.started {
		return
	}
	r.started = true
	startMu.Lock()
	hooks := append([]func(*Context){}, startHooks...)
	startMu.Unlock()
	for _, fn := range hooks {
		fn(r.App)
	}
}
