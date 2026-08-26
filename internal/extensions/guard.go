package extensions

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// Every call from core into extension code goes through this file.
//
// Two hazards, handled differently:
//
//   - A panic is always contained. An extension is compiled into the same
//     process as the agent, so an unguarded panic anywhere in it takes Pando
//     down with it. Containment turns "Pando crashed" into "one feature is
//     broken and said so", which is the only acceptable failure for something
//     optional.
//
//   - A hang is contained only where the call is *declarative*: listing tools,
//     naming panels, answering which topics a subscriber wants, handling an
//     event. Those must return promptly by contract, so a deadline is a
//     correct assumption about them.
//
// Work that is legitimately long is deliberately left without a deadline: an
// extension tool's Run may take minutes for the same reasons `bash` does, and
// a timeout there would kill valid work. Tool calls are cancelled through the
// context the agent already threads, which is the mechanism that knows when
// the user gave up.

// declarativeTimeout bounds the calls described above. It is a constant rather
// than a setting because it is not a tuning knob: it is the boundary between
// "slow" and "wedged", and an extension that needs more than this to list its
// own tools is broken in a way a larger number would only hide.
var declarativeTimeout = 30 * time.Second

// declarativeTimeoutForTest shortens the deadline and returns a function that
// restores it. Only tests call it; the timeout is a constant in every sense
// that matters at run time.
func declarativeTimeoutForTest(d time.Duration) func() {
	prev := declarativeTimeout
	declarativeTimeout = d
	return func() { declarativeTimeout = prev }
}

// guardValue calls fn, containing a panic. ok is false when it panicked, and
// the caller decides what an absent answer means — usually "ignore this
// extension's contribution", never "fail the host".
func guardValue[T any](what string, id extension.ID, fn func() T) (out T, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension call panicked, ignoring its result",
				"extension", ownerName(id), "call", what, "panic", r)
			var zero T
			out, ok = zero, false
		}
	}()
	return fn(), true
}

// ownerName renders the extension an error belongs to. Some call sites reach
// extension code through an adapter that no longer carries the ID; saying so is
// better than logging an empty field that reads like a bug in the logger.
func ownerName(id extension.ID) string {
	if id == "" {
		return "unknown"
	}
	return string(id)
}

// guardErr calls fn, converting a panic into an error so the caller can report
// it the same way it reports a returned failure.
func guardErr(what string, id extension.ID, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("extension %s panicked in %s: %v", id, what, r)
		}
	}()
	return fn()
}

// guardDeclarative runs fn under both guards: a panic is contained and a hang
// is cut short at declarativeTimeout.
//
// The timeout releases the *caller*, not the extension: a wedged goroutine
// cannot be killed in Go, so it is abandoned. That is the honest trade — the
// alternative is a Pando that never finishes building its tool list because
// one extension never returned.
func guardDeclarative[T any](ctx context.Context, what string, id extension.ID, fn func(context.Context) T) (T, bool) {
	callCtx, cancel := context.WithTimeout(ctx, declarativeTimeout)
	defer cancel()

	type result struct {
		value T
		ok    bool
	}
	done := make(chan result, 1) // buffered: an abandoned goroutine must not leak on send
	go func() {
		v, ok := guardValue(what, id, func() T { return fn(callCtx) })
		done <- result{v, ok}
	}()

	select {
	case r := <-done:
		return r.value, r.ok
	case <-callCtx.Done():
		logging.Error("Extension call did not return in time, ignoring it",
			"extension", ownerName(id), "call", what, "timeout", declarativeTimeout)
		var zero T
		return zero, false
	}
}
