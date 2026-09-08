package logging

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// RemoteSink receives every log record the tee handler (see tee_handler.go)
// decides to ship, in addition to the primary (local) handler. It is
// implemented by *internal/telemetry.Shipper; this interface is kept small
// and local to internal/logging so this package never has to import
// internal/telemetry (see the dependency rule in the remote telemetry plan:
// internal/logging must stay free of internal/config and internal/telemetry
// wiring, which instead lives in internal/app).
type RemoteSink interface {
	// Enabled reports whether the sink currently wants records at level —
	// e.g. *internal/telemetry.Shipper checks level against its configured
	// minimum level and whether it has been disabled (by an ingest
	// rejection or a recovered worker panic). The tee handler consults this
	// both from its own Enabled (so slog's log functions can skip building
	// args entirely when neither the primary handler nor the sink wants the
	// record) and from Handle (so a record the sink does not want is never
	// forwarded at all, not even to be dropped inside the sink itself).
	Enabled(level slog.Level) bool
	// Handle is called once per shipped record, already past whatever level
	// filtering the tee handler applies. The sink is responsible for its own
	// additional filtering (e.g. a configured minimum level), batching, and
	// never blocking the caller.
	Handle(ctx context.Context, t time.Time, level slog.Level, msg string, attrs []slog.Attr)
}

// RemoteSinkFlusher is an optional extension a RemoteSink can implement to
// support a short, synchronous flush — used on the panic-recovery path,
// where the process may be about to exit and the sink's normal
// interval/threshold batching would otherwise never run.
type RemoteSinkFlusher interface {
	Flush(ctx context.Context) error
}

// remoteSink is the process-global active remote sink, an atomic pointer so
// Set/active-check/Handle are all safe for concurrent use without a lock.
// It is nil when remote telemetry is not currently enabled. Kept as a
// pointer-to-interface (rather than atomic.Value) so clearing it back to nil
// is a plain Store(nil), which atomic.Value does not allow once a concrete
// type has been stored.
var remoteSink atomic.Pointer[RemoteSink]

// SetRemoteSink installs (or, with a nil argument, clears) the process-wide
// remote sink. Called by internal/app's telemetry runtime when the user
// enables/disables remote logging, live, with no process restart required.
func SetRemoteSink(sink RemoteSink) {
	if sink == nil {
		remoteSink.Store(nil)
		return
	}
	remoteSink.Store(&sink)
}

// getRemoteSink returns the active remote sink, or nil when none is set.
func getRemoteSink() RemoteSink {
	p := remoteSink.Load()
	if p == nil {
		return nil
	}
	return *p
}

// RemoteSinkActive reports whether a remote sink is currently installed.
func RemoteSinkActive() bool {
	return getRemoteSink() != nil
}

// FlushRemoteSink synchronously flushes the active remote sink, bounded by
// ctx, when one is set and it implements RemoteSinkFlusher. It is a no-op
// (returns nil) when there is no active sink or the sink does not support a
// synchronous flush. Used by RecoverPanic to push a panic record out before
// the process potentially exits.
func FlushRemoteSink(ctx context.Context) error {
	sink := getRemoteSink()
	if sink == nil {
		return nil
	}
	flusher, ok := sink.(RemoteSinkFlusher)
	if !ok {
		return nil
	}
	return flusher.Flush(ctx)
}
