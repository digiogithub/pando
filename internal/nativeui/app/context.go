// Package app is the core of the native desktop UI: the app-wide Context
// handed to every view, the registry of areas (chat, settings, files, ...),
// the router and the window layout (sidebar, header, content). Each area
// lives in its own package under internal/nativeui/app/<area> and registers
// itself from init(); see internal/nativeui/README.md for the conventions.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Dispatcher posts work to the UI thread. *mygo.Window implements it: Update
// runs fn on the main thread then redraws, Invalidate only redraws. Both are
// safe from any goroutine and return without waiting.
type Dispatcher interface {
	Update(fn func())
	Invalidate()
}

// Context is the app-wide context every view receives. Its fields are set
// once at start-up; mutate view state only on the UI thread (in the view, or
// through Update from goroutines).
type Context struct {
	// Ctx is canceled when the app shuts down: derive goroutine contexts
	// from it.
	Ctx context.Context
	// Client talks to the Pando server.
	Client *client.Client
	// Server describes the server (version, startup mode).
	Server client.ServerInfo
	// CWD is the working directory of the server, when known.
	CWD string
	// Router navigates between areas (paths of the registered areas).
	Router *ui.Router
	// Toasts queues toasts from any goroutine.
	Toasts *kit.Toaster
	// Theme selects family, mode and accent; read every frame.
	Theme kit.Settings
	// Log is the UI logger.
	Log *slog.Logger

	// parent is set on contexts made by ForClient; they borrow its dispatcher.
	parent *Context

	// shell is the window state (see Shell()); only root contexts own one.
	shell *Shell

	dispMu sync.RWMutex
	disp   Dispatcher
}

// newRouter returns the router of a window or workspace. Pages show at
// once, as in the Web UI: an area is a sidebar destination and its
// sub-routes (a chat session, a settings section) replace each other in
// place. A slide would build the area's view twice per frame (see
// Root.leavingSameArea), and a fade leaves the page transparent while the
// window gets no frames (hidden, occluded or locked screen on macOS).
func newRouter() *ui.Router {
	r := ui.NewRouter(DefaultPath)
	r.Transition = ui.TransitionNone
	return r
}

// NewContext returns a context with defaults for what opts leaves empty.
func NewContext(ctx context.Context, c *client.Client) *Context {
	if ctx == nil {
		ctx = context.Background()
	}
	a := &Context{
		Ctx:    ctx,
		Client: c,
		Router: newRouter(),
		Toasts: &kit.Toaster{},
		Theme:  kit.DefaultSettings,
		Log:    slog.Default().With("component", "nativeui"),
		shell:  &Shell{},
	}
	a.Toasts.Invalidate = a.Invalidate
	return a
}

// SetDispatcher connects the context to its window. Until then Update runs
// nothing (there is no UI to update) and Invalidate is a no-op.
func (a *Context) SetDispatcher(d Dispatcher) {
	a.dispMu.Lock()
	a.disp = d
	a.dispMu.Unlock()
}

func (a *Context) dispatcher() Dispatcher {
	a.dispMu.RLock()
	d := a.disp
	a.dispMu.RUnlock()
	if d == nil && a.parent != nil {
		return a.parent.dispatcher()
	}
	return d
}

// ForClient returns a context that talks to another server (a project's
// child instance through the parent proxy, see client.Client.Derive) while
// sharing everything else: the window dispatcher, toasts, ctx and logger.
// Areas are instantiated against it with Area.New(child), which is how a
// project workspace hosts chat, files, ... for another project. The child has
// its own Router (the routes of the embedded areas must not move the main
// window) and its own Server info and CWD. Its Theme is not used: the root
// view applies the theme once per frame and kit.Current() is global.
func (a *Context) ForClient(c *client.Client, cwd string) *Context {
	return &Context{
		Ctx:    a.Ctx,
		Client: c,
		CWD:    cwd,
		Router: newRouter(),
		Toasts: a.Toasts,
		Theme:  a.Theme,
		Log:    a.Log,
		parent: a,
	}
}

// IsChild reports whether the context is a project workspace's (made by
// ForClient): its areas are embedded in a tab, without the window header.
func (a *Context) IsChild() bool { return a.parent != nil }

// Update runs fn on the UI thread and redraws. Background goroutines must
// change view state only through it.
func (a *Context) Update(fn func()) {
	if d := a.dispatcher(); d != nil {
		d.Update(fn)
	}
}

// Invalidate asks for a redraw.
func (a *Context) Invalidate() {
	if d := a.dispatcher(); d != nil {
		d.Invalidate()
	}
}

// T translates a key of the Web UI catalogs (i18n.T).
func (a *Context) T(key string, args ...any) string { return i18n.T(key, args...) }

// Go runs fn on a goroutine with the app context, logging a panic instead of
// crashing the UI.
func (a *Context) Go(fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.Log.Error("background task panicked", "panic", r, "stack", string(debug.Stack()))
				a.Toasts.Error("Internal error", fmt.Sprint(r))
			}
		}()
		fn(a.Ctx)
	}()
}

// Navigate pushes a path on the router (UI thread).
func (a *Context) Navigate(path string) { a.Router.Push(path) }

// Batch coalesces items pushed from goroutines into one UI update per
// frame: Push queues an item and schedules a single Update that hands every
// item queued meanwhile to apply. It is how streams feed the UI without one
// frame per token.
type Batch[T any] struct {
	mu        sync.Mutex
	items     []T
	scheduled bool
	update    func(func())
	apply     func([]T)
}

// NewBatch returns a batch applying items on the UI thread through update
// (usually Context.Update).
func NewBatch[T any](update func(func()), apply func([]T)) *Batch[T] {
	return &Batch[T]{update: update, apply: apply}
}

// Push queues an item. Safe from any goroutine.
func (b *Batch[T]) Push(v T) {
	b.mu.Lock()
	b.items = append(b.items, v)
	if b.scheduled {
		b.mu.Unlock()
		return
	}
	b.scheduled = true
	b.mu.Unlock()
	b.update(b.flush)
}

func (b *Batch[T]) flush() {
	b.mu.Lock()
	items := b.items
	b.items = nil
	b.scheduled = false
	b.mu.Unlock()
	if len(items) > 0 {
		b.apply(items)
	}
}

// QueueDispatcher is a Dispatcher for tests and headless use: Update queues
// fn until Drain runs the queue on the caller's goroutine.
type QueueDispatcher struct {
	mu    sync.Mutex
	queue []func()
	inval int
}

// Update queues fn.
func (q *QueueDispatcher) Update(fn func()) {
	q.mu.Lock()
	q.queue = append(q.queue, fn)
	q.mu.Unlock()
}

// Invalidate counts a redraw request.
func (q *QueueDispatcher) Invalidate() {
	q.mu.Lock()
	q.inval++
	q.mu.Unlock()
}

// Drain runs the queued functions (and those they queue) and reports how
// many ran.
func (q *QueueDispatcher) Drain() int {
	n := 0
	for {
		q.mu.Lock()
		fns := q.queue
		q.queue = nil
		q.mu.Unlock()
		if len(fns) == 0 {
			return n
		}
		for _, fn := range fns {
			fn()
			n++
		}
	}
}
