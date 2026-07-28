package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSEWriterFrameFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	if err := sse.Write(NewRunStarted("t1", "r1")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sse.Comment("keep-alive"); err != nil {
		t.Fatalf("comment: %v", err)
	}

	body := rec.Body.String()
	// AG-UI carries the discriminator inside the JSON payload, so frames are
	// bare `data:` lines with no `event:` field.
	if strings.Contains(body, "event:") {
		t.Fatalf("unexpected SSE event field: %q", body)
	}
	if !strings.HasPrefix(body, `data: {"type":"RUN_STARTED"`) {
		t.Fatalf("unexpected first frame: %q", body)
	}
	if !strings.Contains(body, "\n\n: keep-alive\n\n") {
		t.Fatalf("heartbeat comment missing or malformed: %q", body)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("proxy buffering not disabled: %q", got)
	}
}

func TestSSEWriterRefusesWritesAfterClose(t *testing.T) {
	sse, err := NewSSEWriter(httptest.NewRecorder())
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	sse.Close()
	if err := sse.Write(NewRunFinished("t1", "r1", OutcomeSuccess, nil)); err == nil {
		t.Fatal("expected a write after Close to fail")
	}
}

// nonFlushingWriter is an http.ResponseWriter that cannot stream.
type nonFlushingWriter struct{ http.ResponseWriter }

func TestSSEWriterRequiresFlusher(t *testing.T) {
	if _, err := NewSSEWriter(nonFlushingWriter{httptest.NewRecorder()}); err != ErrStreamingUnsupported {
		t.Fatalf("expected ErrStreamingUnsupported, got %v", err)
	}
}
