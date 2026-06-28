package agent

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/ponytail"
)

func TestSetAndGetPonytailMode(t *testing.T) {
	const sid = "sess-ponytail-1"
	t.Cleanup(func() { SetPonytailMode(sid, ponytail.ModeOff) })

	if got := PonytailMode(sid); got.IsActive() {
		t.Fatalf("expected off by default, got %v", got)
	}

	SetPonytailMode(sid, ponytail.ModeUltra)
	if got := PonytailMode(sid); got != ponytail.ModeUltra {
		t.Fatalf("expected ultra, got %v", got)
	}

	// off clears the entry.
	SetPonytailMode(sid, ponytail.ModeOff)
	if got := PonytailMode(sid); got.IsActive() {
		t.Fatalf("expected cleared, got %v", got)
	}
}

func TestPonytailModeForContext(t *testing.T) {
	const sid = "sess-ponytail-2"
	t.Cleanup(func() { SetPonytailMode(sid, ponytail.ModeOff) })

	SetPonytailMode(sid, ponytail.ModeFull)
	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	if got := ponytailModeForContext(ctx); got != ponytail.ModeFull {
		t.Fatalf("expected full from ctx, got %v", got)
	}

	if got := ponytailModeForContext(context.Background()); got.IsActive() {
		t.Fatalf("expected off without session id, got %v", got)
	}
}
