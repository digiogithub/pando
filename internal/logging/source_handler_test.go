package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSourceLoggerAddsSourceBelowWarn(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	l := SourceLogger()
	l.Debug("dbg", "k", "v")
	if out := buf.String(); !strings.Contains(out, "source=") || !strings.Contains(out, "source_handler_test.go:") || !strings.Contains(out, "k=v") {
		t.Fatalf("debug record missing source or attrs: %q", out)
	}

	buf.Reset()
	l.Warn("wrn")
	if out := buf.String(); strings.Contains(out, "source=") {
		t.Fatalf("warn record should not carry source: %q", out)
	}

	buf.Reset()
	l.With("a", 1).WithGroup("g").Info("grp", "b", 2)
	if out := buf.String(); !strings.Contains(out, "a=1") || !strings.Contains(out, "g.b=2") {
		t.Fatalf("With/WithGroup not replayed: %q", out)
	}
}
