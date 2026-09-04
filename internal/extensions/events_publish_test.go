package extensions

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/pkg/extension"
)

// detachSink makes sure a test never leaves the process-wide sink installed.
func detachSink(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { extevents.SetSink(context.Background(), nil) })
}

func TestStartEventPublisherDeliversHostEvents(t *testing.T) {
	detachSink(t)
	sub := &subExt{baseExt: baseExt{id: "sink.acme"}, topics: []string{extension.TopicTool}}
	mgr := managerWith(t, sub)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	StartEventPublisher(ctx, mgr)

	extevents.ToolStarted("s1", "call-1", "bash", "coder")
	extevents.ToolCompleted("s1", "call-1", "bash", "coder", 2*time.Millisecond, nil)
	// A topic the subscriber did not ask for must not reach it.
	extevents.SkillActivated("s1", "design", "coder", "auto")

	if !waitFor(t, func() bool { return len(sub.seen()) == 2 }) {
		t.Fatalf("expected two tool events, got %d", len(sub.seen()))
	}
	for _, ev := range sub.seen() {
		if ev.Topic != extension.TopicTool {
			t.Errorf("subscriber received topic %q", ev.Topic)
		}
	}
}

// A panicking subscriber must not stop the publisher for anyone else, exactly
// as with the brokered topics.
func TestStartEventPublisherContainsPanic(t *testing.T) {
	detachSink(t)
	bad := &subExt{baseExt: baseExt{id: "sink.bad"}, panics: true}
	good := &subExt{baseExt: baseExt{id: "sink.good"}}
	mgr := managerWith(t, bad, good)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	StartEventPublisher(ctx, mgr)

	extevents.SkillActivated("s1", "design", "coder", "auto")
	extevents.ProviderAccountAdded("acc1", "anthropic", false)

	if !waitFor(t, func() bool { return len(good.seen()) == 2 }) {
		t.Fatalf("publisher stopped after a panic: %d events", len(good.seen()))
	}
}

// With no subscriber loaded nothing is installed, so every publish site in
// core stays at one atomic load.
func TestStartEventPublisherIsNoOpWithoutSubscribers(t *testing.T) {
	detachSink(t)
	mgr := managerWith(t, &provExt{baseExt: baseExt{id: "tools.acme"}})

	StartEventPublisher(context.Background(), mgr)
	if extevents.Enabled() {
		t.Fatal("a build with no subscriber installed a sink")
	}
}
