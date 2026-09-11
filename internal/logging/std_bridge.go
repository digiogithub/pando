package logging

import (
	"context"
	"log"
	"log/slog"
	"strings"
)

// NewStdLogger returns a standard library *log.Logger whose output is routed
// into the process-wide slog logger (slog.Default at write time, so it follows
// any later slog.SetDefault and live level changes) at the given level, tagged
// with a "component" attribute.
//
// It exists for packages that still log through *log.Logger (e.g. the ACP
// agent and transports): their lines then reach the same destinations as the
// rest of Pando's logs — the log file / in-memory writer and, through the tee
// handler, remote telemetry — instead of being discarded or written to
// stderr. Nothing is ever written to stdout, so it is safe in ACP stdio mode.
func NewStdLogger(component string, level slog.Level) *log.Logger {
	return log.New(&slogWriter{component: component, level: level}, "", 0)
}

// slogWriter adapts io.Writer to slog: each Write is one log line produced by
// *log.Logger (which always calls Write once per formatted entry).
type slogWriter struct {
	component string
	level     slog.Level
}

func (w *slogWriter) Write(p []byte) (int, error) {
	logger := slog.Default()
	ctx := context.Background()
	if !logger.Enabled(ctx, w.level) {
		return len(p), nil
	}
	msg := strings.TrimRight(string(p), "\r\n")
	if msg == "" {
		return len(p), nil
	}
	logger.Log(ctx, w.level, msg, "component", w.component)
	return len(p), nil
}
