package chat

import (
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
)

// Heartbeats keep a quiet run alive (the border trail runs, the hint counts
// the agent's silence); with nothing at all for stallAfter the composer
// reconnects once, then says the connection may be down.
func TestComposerLivenessFromHeartbeats(t *testing.T) {
	now := time.Unix(1000, 0)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	ch, tt := newWideChat(t, 1200, 800)
	cv := ch.conv()
	cv.BeginAttach()
	tt.Frame()
	if cv.liveness() != liveActive {
		t.Fatal("a fresh stream is active")
	}
	beat := func() {
		ch.applyEvents([]streamMsg{{conv: cv, tag: cv.streamTag, ev: client.StreamEvent{Type: client.EventHeartbeat, Running: true}}})
	}
	now = now.Add(15 * time.Second)
	beat()
	now = now.Add(15 * time.Second)
	tt.Frame()
	if cv.liveness() != liveActive {
		t.Fatal("a heartbeat 15 s ago keeps the run alive")
	}
	if quiet := ch.app.T("chat.composer.quiet", "seconds", 30); !tt.HasText(quiet) {
		t.Errorf("missing %q: %q", quiet, tt.Texts())
	}
	now = now.Add(10 * time.Second)
	tt.Frame() // stalled: reconnects on its own first
	if !cv.reconnected {
		t.Fatal("the first stall must reconnect")
	}
	tt.Frame()
	stalled := ch.app.T("chat.composer.stalled", "seconds", 25)
	if !tt.HasText(stalled) || !tt.HasText(ch.app.T("chat.composer.reconnect")) {
		t.Fatalf("missing stall notice %q: %q", stalled, tt.Texts())
	}
	beat()
	tt.Frame()
	if cv.liveness() != liveActive || tt.HasText(stalled) {
		t.Error("a heartbeat must clear the stall")
	}
	cv.Finish()
	if cv.liveness() != liveNone {
		t.Error("a finished run has no liveness")
	}
}
