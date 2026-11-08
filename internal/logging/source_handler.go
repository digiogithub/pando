package logging

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
)

// SourceLogger returns a *slog.Logger for packages that take a logger instead
// of calling this package's functions (the rag module's kb store). Records
// go to whatever slog.Default() is at the time they are written, so a later
// slog.SetDefault or level change applies, and Debug and Info records get
// the same "source" attribute (file:line of the call) that Debug and Info
// in this package add. Warn and Error records are passed through unchanged,
// as Warn and Error here do.
func SourceLogger() *slog.Logger {
	return slog.New(sourceHandler{})
}

// sourceHandler delegates to the current default handler, adding "source"
// to records below Warn. ops replays WithAttrs/WithGroup calls, in order, on
// the default handler each time it is resolved.
type sourceHandler struct {
	ops []func(slog.Handler) slog.Handler
}

func (h sourceHandler) inner() slog.Handler {
	in := slog.Default().Handler()
	for _, op := range h.ops {
		in = op(in)
	}
	return in
}

func (h sourceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner().Enabled(ctx, level)
}

func (h sourceHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < slog.LevelWarn {
		source := "unknown"
		if r.PC != 0 {
			frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
			if frame.File != "" {
				source = fmt.Sprintf("%s:%d", frame.File, frame.Line)
			}
		}
		rec := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
		rec.AddAttrs(slog.String("source", source))
		r.Attrs(func(a slog.Attr) bool {
			rec.AddAttrs(a)
			return true
		})
		r = rec
	}
	return h.inner().Handle(ctx, r)
}

func (h sourceHandler) with(op func(slog.Handler) slog.Handler) sourceHandler {
	ops := make([]func(slog.Handler) slog.Handler, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	return sourceHandler{ops: append(ops, op)}
}

func (h sourceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(func(in slog.Handler) slog.Handler { return in.WithAttrs(attrs) })
}

func (h sourceHandler) WithGroup(name string) slog.Handler {
	return h.with(func(in slog.Handler) slog.Handler { return in.WithGroup(name) })
}
