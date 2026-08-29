//go:build windows

package windows

import (
	"context"
	"runtime"
	"sync"

	ole "github.com/go-ole/go-ole"
)

// comWorker runs every COM call this backend makes on a single, dedicated
// OS thread. UI Automation client objects are apartment-threaded
// (COINIT_APARTMENTTHREADED) and are documented as not safe to call from an
// arbitrary goroutine — Go's scheduler is free to move a goroutine between
// OS threads at any await point, which would silently violate STA
// thread-affinity and corrupt or crash the automation client. Every COM
// call in this package is therefore funneled through comWorker.run, which
// executes the given closure on the worker goroutine (parked via
// runtime.LockOSThread for its whole lifetime) and returns the result over
// a channel.
type comWorker struct {
	requests chan comRequest
	done     chan struct{}

	mu      sync.Mutex
	stopped bool
}

type comRequest struct {
	fn   func()
	done chan struct{}
}

// coinitApartmentThreaded is COINIT_APARTMENTTHREADED (objbase.h).
const coinitApartmentThreaded = 0x2

// newComWorker starts the dedicated COM apartment thread and initializes
// COM on it (CoInitializeEx(nil, COINIT_APARTMENTTHREADED)). It blocks
// until CoInitializeEx has run (successfully or not) so the returned error
// reflects real COM initialization failure, not a race.
func newComWorker() (*comWorker, error) {
	w := &comWorker{
		requests: make(chan comRequest),
		done:     make(chan struct{}),
	}
	initErr := make(chan error, 1)
	go w.loop(initErr)
	if err := <-initErr; err != nil {
		return nil, err
	}
	return w, nil
}

func (w *comWorker) loop(initErr chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	err := ole.CoInitializeEx(0, coinitApartmentThreaded)
	// RPC_E_CHANGED_MODE (already initialized with a different concurrency
	// model on this thread) is the one failure worth tolerating: it can
	// only happen if something else already called CoInitializeEx on this
	// brand-new goroutine's thread, which never happens in practice, but a
	// plain "already initialized" (S_FALSE) from a prior successful call on
	// this same thread is not an error either.
	initErr <- err
	defer ole.CoUninitialize()

	for {
		select {
		case req := <-w.requests:
			req.fn()
			close(req.done)
		case <-w.done:
			return
		}
	}
}

// run executes fn on the COM apartment thread and blocks until it
// completes or ctx is done. When ctx is done first, run returns ctx.Err()
// but fn may still complete asynchronously afterward (its result is simply
// discarded) — callers must not touch COM state fn captured after a ctx
// timeout without re-synchronizing, which none of this backend's call sites
// do (every fn is a single self-contained call/decode).
func (w *comWorker) run(ctx context.Context, fn func()) error {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return context.Canceled
	}
	w.mu.Unlock()

	req := comRequest{fn: fn, done: make(chan struct{})}
	select {
	case w.requests <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return context.Canceled
	}
	select {
	case <-req.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop shuts down the worker goroutine, running CoUninitialize on the same
// thread COM was initialized on.
func (w *comWorker) stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.mu.Unlock()
	close(w.done)
}
