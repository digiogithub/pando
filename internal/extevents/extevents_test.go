package extevents

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/pkg/extension"
)

// collector is a sink that records what it was given.
type collector struct {
	mu     sync.Mutex
	events []extension.Event
}

func (c *collector) sink(ev extension.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *collector) seen() []extension.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]extension.Event(nil), c.events...)
}

// waitFor polls until cond holds; delivery is asynchronous by design, so a
// bare assertion would be a race.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// installSink wires a collector for the duration of one test and detaches it
// afterwards, so tests never leak a sink into each other.
func installSink(t *testing.T) *collector {
	t.Helper()
	c := &collector{}
	ctx, cancel := context.WithCancel(context.Background())
	SetSink(ctx, c.sink)
	t.Cleanup(func() {
		cancel()
		SetSink(context.Background(), nil)
	})
	return c
}

func TestPublishIsNoOpWithoutSink(t *testing.T) {
	SetSink(context.Background(), nil)
	if Enabled() {
		t.Fatal("Enabled with no sink installed")
	}
	// The point is that none of these block or panic.
	ToolStarted("s1", "c1", "bash", "coder")
	ToolCompleted("s1", "c1", "bash", "coder", time.Second, nil)
	MCPHandshake("srv", "stdio", "test", nil, false)
	SkillActivated("s1", "design", "coder", "auto")
	ProviderAccountAdded("acc1", "anthropic", false)
}

func TestToolPairIsPublished(t *testing.T) {
	c := installSink(t)

	ToolStarted("s1", "call-1", "bash", "coder")
	ToolCompleted("s1", "call-1", "bash", "coder", 1500*time.Millisecond, nil)

	if !waitFor(t, func() bool { return len(c.seen()) == 2 }) {
		t.Fatalf("expected two events, got %d", len(c.seen()))
	}
	start, done := c.seen()[0], c.seen()[1]
	if start.Topic != extension.TopicTool || start.Type != extension.EventStarted {
		t.Errorf("start = %+v", start)
	}
	if start.ID != "call-1" || start.SessionID != "s1" {
		t.Errorf("start ids = %+v", start)
	}
	if start.Time.IsZero() {
		t.Error("start carries no timestamp")
	}
	if done.Type != extension.EventCompleted || done.ID != "call-1" {
		t.Errorf("done = %+v", done)
	}
	if done.Payload["ok"] != true {
		t.Errorf("ok = %v", done.Payload["ok"])
	}
	if done.Payload["durationMs"] != float64(1500) {
		t.Errorf("durationMs = %v", done.Payload["durationMs"])
	}
	if _, has := done.Payload["error"]; has {
		t.Error("a successful call must not carry an error")
	}
	if _, has := done.Payload["input"]; has {
		t.Error("tool arguments must never be published")
	}
}

func TestToolCompletedCarriesFailure(t *testing.T) {
	c := installSink(t)

	ToolCompleted("s1", "call-2", "edit", "coder", time.Millisecond, errors.New("permission denied"))

	if !waitFor(t, func() bool { return len(c.seen()) == 1 }) {
		t.Fatal("event never arrived")
	}
	ev := c.seen()[0]
	if ev.Payload["ok"] != false {
		t.Errorf("ok = %v", ev.Payload["ok"])
	}
	if ev.Payload["error"] != "permission denied" {
		t.Errorf("error = %v", ev.Payload["error"])
	}
}

func TestMCPHandshakeTypes(t *testing.T) {
	c := installSink(t)

	MCPHandshake("ok-server", "stdio", "test", nil, false)
	MCPHandshake("auth-server", "http", "test", errors.New("401"), true)
	MCPHandshake("bad-server", "sse", "test", errors.New("dial failed"), false)

	if !waitFor(t, func() bool { return len(c.seen()) == 3 }) {
		t.Fatalf("expected three events, got %d", len(c.seen()))
	}
	got := c.seen()
	want := []extension.EventType{extension.EventConnected, extension.EventAuthRequired, extension.EventFailed}
	for i, ev := range got {
		if ev.Topic != extension.TopicMCP {
			t.Errorf("event %d topic = %q", i, ev.Topic)
		}
		if ev.Type != want[i] {
			t.Errorf("event %d type = %q, want %q", i, ev.Type, want[i])
		}
	}
	if got[0].Payload["transport"] != "stdio" || got[0].ID != "ok-server" {
		t.Errorf("connected payload = %+v", got[0])
	}
	if _, has := got[0].Payload["error"]; has {
		t.Error("a successful handshake must not carry an error")
	}
}

func TestSkillAndProviderEvents(t *testing.T) {
	c := installSink(t)

	SkillActivated("s1", "design", "coder", "auto")
	ProviderAccountAdded("acc1", "anthropic", false)
	ProviderAccountUpdated("acc1", "anthropic", true)
	ProviderAccountRemoved("acc1", "anthropic")

	if !waitFor(t, func() bool { return len(c.seen()) == 4 }) {
		t.Fatalf("expected four events, got %d", len(c.seen()))
	}
	got := c.seen()
	if got[0].Topic != extension.TopicSkill || got[0].Type != extension.EventActivated || got[0].ID != "design" {
		t.Errorf("skill event = %+v", got[0])
	}
	wantTypes := []extension.EventType{extension.EventCreated, extension.EventUpdated, extension.EventDeleted}
	for i, ev := range got[1:] {
		if ev.Topic != extension.TopicProvider {
			t.Errorf("provider event %d topic = %q", i, ev.Topic)
		}
		if ev.Type != wantTypes[i] {
			t.Errorf("provider event %d type = %q, want %q", i, ev.Type, wantTypes[i])
		}
		if ev.Payload["providerType"] != "anthropic" || ev.ID != "acc1" {
			t.Errorf("provider event %d = %+v", i, ev)
		}
		for _, forbidden := range []string{"apiKey", "api_key", "token", "baseURL"} {
			if _, has := ev.Payload[forbidden]; has {
				t.Errorf("provider event carries %q", forbidden)
			}
		}
	}
	if got[2].Payload["disabled"] != true {
		t.Errorf("disabled = %v", got[2].Payload["disabled"])
	}
}

// A publisher must never block, even when nothing is draining the buffer.
func TestPublishDropsInsteadOfBlocking(t *testing.T) {
	blocked := make(chan struct{})
	released := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		close(released)
		cancel()
		SetSink(context.Background(), nil)
	})
	var once sync.Once
	SetSink(ctx, func(extension.Event) {
		once.Do(func() { close(blocked) })
		<-released
	})

	// Fill the buffer several times over with the sink wedged. Publishing has
	// to return regardless; the excess is dropped.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < bufferSize*3; i++ {
			ToolStarted("s1", "c", "bash", "coder")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked behind a wedged sink")
	}
	<-blocked
}

func TestSetSinkNilDetaches(t *testing.T) {
	c := installSink(t)
	ToolStarted("s1", "c1", "bash", "coder")
	if !waitFor(t, func() bool { return len(c.seen()) == 1 }) {
		t.Fatal("event never arrived")
	}

	SetSink(context.Background(), nil)
	if Enabled() {
		t.Fatal("Enabled after detaching")
	}
	ToolStarted("s1", "c2", "bash", "coder")
	time.Sleep(50 * time.Millisecond)
	if len(c.seen()) != 1 {
		t.Errorf("detached sink still received events: %d", len(c.seen()))
	}
}
