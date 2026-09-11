package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func withDefaultSlog(t *testing.T, h slog.Handler) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestNewStdLoggerRoutesToSlog(t *testing.T) {
	var buf bytes.Buffer
	withDefaultSlog(t, slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	logger := NewStdLogger("acp", slog.LevelDebug)
	logger.Printf("[ACP AGENT] Prompt request: SessionID=%s", "sess-1")

	out := buf.String()
	if !strings.Contains(out, "level=DEBUG") {
		t.Fatalf("expected DEBUG record, got %q", out)
	}
	if !strings.Contains(out, "Prompt request: SessionID=sess-1") {
		t.Fatalf("expected message in record, got %q", out)
	}
	if !strings.Contains(out, "component=acp") {
		t.Fatalf("expected component attr, got %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected exactly one record line, got %q", out)
	}
}

func TestNewStdLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	withDefaultSlog(t, slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	NewStdLogger("acp", slog.LevelDebug).Printf("trace line")
	if buf.Len() != 0 {
		t.Fatalf("debug line must be dropped at info level, got %q", buf.String())
	}
}

func TestNewStdLoggerFollowsSetDefault(t *testing.T) {
	logger := NewStdLogger("acp", slog.LevelInfo)

	var buf bytes.Buffer
	withDefaultSlog(t, slog.NewTextHandler(&buf, nil))
	logger.Printf("after swap")
	if !strings.Contains(buf.String(), "after swap") {
		t.Fatalf("logger created before SetDefault must use the new default, got %q", buf.String())
	}
}
