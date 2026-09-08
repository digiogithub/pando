package logging

import (
	"context"
	"log/slog"
	"strings"

	"github.com/digiogithub/pando/internal/telemetry"
)

// teeHandler forwards every record to a primary (local) slog.Handler
// unchanged, and additionally — when a remote sink is installed (see
// remote_sink.go) — to that sink, flattening any WithAttrs/WithGroup state
// into a dotted-key attribute list the sink can redact and ship.
//
// It must never block or panic: the primary handler is whatever the caller
// configured (file/pubsub-writer text handler), and the remote sink's own
// Handle (internal/telemetry.Shipper.Handle) is documented as non-blocking
// and panic-safe on its own.
type teeHandler struct {
	primary slog.Handler

	// groupPrefix is the dot-joined path of open groups (from WithGroup
	// calls), applied to attribute keys before they reach the remote sink.
	// The primary handler manages its own group nesting internally via its
	// own WithGroup, so this is only needed for the flattened sink view.
	groupPrefix string

	// sinkAttrs holds every attribute accumulated via WithAttrs so far, with
	// whatever groupPrefix was active at the time already baked into each
	// key. Prepended to a record's own attrs when forwarding to the sink.
	sinkAttrs []slog.Attr
}

// NewTeeHandler wraps primary so every record also reaches the process-wide
// remote sink (internal/logging.SetRemoteSink) when one is active. Wrap it
// around the handler passed to slog.New in every place a *slog.Logger is
// constructed as Pando's default logger, so remote shipping survives a
// config.Reload() (which repeats that construction and calls
// slog.SetDefault again) without any special-casing.
func NewTeeHandler(primary slog.Handler) slog.Handler {
	return &teeHandler{primary: primary}
}

// Enabled reports whether either destination actually wants the record at
// this level: the primary handler's own level gate, or the active remote
// sink's own Enabled (e.g. internal/telemetry.Shipper.Enabled checks
// Options.MinLevel and whether the shipper has been disabled). Unlike the
// sink-is-merely-active check this replaced, a sink configured for a higher
// minimum level (or currently disabled) no longer makes every level
// "enabled" — in particular, a Debug record is only built and dispatched at
// all when either the primary or the sink is actually willing to take it at
// Debug, which is what keeps a sink whose effective minimum level is Info
// (see internal/app's telemetry runtime, which clamps MinLevel to Info
// unless the app-wide Debug flag is also on) from ever seeing Debug-level
// content merely because remote telemetry happens to be enabled.
func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.primary.Enabled(ctx, level) {
		return true
	}
	sink := getRemoteSink()
	return sink != nil && sink.Enabled(level)
}

// Handle dispatches r to the primary handler (only if it actually wants this
// level) and, when a remote sink is set and also wants this level, to that
// sink as well — unless the record is marked with telemetry.SkipAttrKey,
// which keeps the shipper's own diagnostic logs (e.g. "shipping disabled")
// from being re-shipped in a feedback loop.
func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if h.primary.Enabled(ctx, r.Level) {
		err = h.primary.Handle(ctx, r.Clone())
	}

	sink := getRemoteSink()
	if sink == nil || !sink.Enabled(r.Level) {
		return err
	}

	recordAttrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		recordAttrs = append(recordAttrs, a)
		return true
	})
	recordAttrs = prefixAttrs(h.groupPrefix, recordAttrs)

	all := make([]slog.Attr, 0, len(h.sinkAttrs)+len(recordAttrs))
	all = append(all, h.sinkAttrs...)
	all = append(all, recordAttrs...)

	if containsSkipAttr(all) {
		return err
	}

	sink.Handle(ctx, r.Time, r.Level, r.Message, all)
	return err
}

// WithAttrs delegates to the primary handler for local formatting, and
// separately records the attrs (with the current group prefix already
// applied to their keys) so a later Handle can prepend them to the sink's
// flattened attribute list.
func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := &teeHandler{
		primary:     h.primary.WithAttrs(attrs),
		groupPrefix: h.groupPrefix,
		sinkAttrs:   append(append([]slog.Attr{}, h.sinkAttrs...), prefixAttrs(h.groupPrefix, attrs)...),
	}
	return next
}

// WithGroup delegates to the primary handler, and extends the dotted group
// prefix used to flatten keys for the remote sink.
func (h *teeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	prefix := name
	if h.groupPrefix != "" {
		prefix = h.groupPrefix + "." + name
	}
	return &teeHandler{
		primary:     h.primary.WithGroup(name),
		groupPrefix: prefix,
		sinkAttrs:   h.sinkAttrs,
	}
}

// prefixAttrs returns attrs with prefix+"." prepended to each non-empty key.
// A group-kind attr only needs its own top-level key renamed this way: the
// remote sink's Record builder (internal/telemetry.NewRecord) already
// recurses into nested slog.Group values, joining keys with ".", so renaming
// the outer key is enough to produce the fully dotted path once it recurses.
func prefixAttrs(prefix string, attrs []slog.Attr) []slog.Attr {
	if prefix == "" || len(attrs) == 0 {
		return attrs
	}
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		if a.Key == "" {
			out[i] = a
			continue
		}
		out[i] = slog.Attr{Key: prefix + "." + a.Key, Value: a.Value}
	}
	return out
}

// containsSkipAttr reports whether attrs carries telemetry.SkipAttrKey,
// anywhere at the top level or nested inside a group value. Matching is on
// the key's last dot-separated segment via isSkipAttrKey, not an exact
// match: WithGroup prefixing (see prefixAttrs) turns a top-level
// "$_telemetry_skip" into "g.$_telemetry_skip" once a group is open, and an
// exact-match check would silently stop recognizing the marker — letting a
// record meant to be skipped slip through to the sink — the moment a caller
// logs it through a grouped logger.
func containsSkipAttr(attrs []slog.Attr) bool {
	for _, a := range attrs {
		if isSkipAttrKey(a.Key) {
			return true
		}
		if v := a.Value.Resolve(); v.Kind() == slog.KindGroup {
			if containsSkipAttr(v.Group()) {
				return true
			}
		}
	}
	return false
}

// isSkipAttrKey reports whether key is telemetry.SkipAttrKey, either bare or
// with a dot-joined group prefix baked in ("g.$_telemetry_skip").
func isSkipAttrKey(key string) bool {
	return key == telemetry.SkipAttrKey || strings.HasSuffix(key, "."+telemetry.SkipAttrKey)
}
