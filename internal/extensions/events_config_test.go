package extensions

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/pkg/extension"
)

func TestForwardConfigEventsDeliversOverlayApplied(t *testing.T) {
	sub := &subExt{baseExt: baseExt{id: "policy.acme"}, topics: []string{extension.TopicConfig}}
	mgr := managerWith(t, sub)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ForwardConfigEvents(ctx, mgr)

	config.Bus.Publish(config.ConfigChangeEvent{
		Section:     config.OverlaySource,
		Event:       config.EventOverlayApplied,
		ChangedKeys: []string{"tui.theme", "agents.coder.model"},
		Source:      config.OverlaySource,
		Timestamp:   time.Now(),
	})

	if !waitFor(t, func() bool { return len(sub.seen()) == 1 }) {
		t.Fatal("overlay_applied never reached the subscriber")
	}
	ev := sub.seen()[0]
	if ev.Topic != extension.TopicConfig {
		t.Errorf("topic = %q, want %q", ev.Topic, extension.TopicConfig)
	}
	if ev.Type != extension.EventOverlayApplied {
		t.Errorf("type = %q, want %q", ev.Type, extension.EventOverlayApplied)
	}
	if ev.Payload["source"] != config.OverlaySource {
		t.Errorf("source = %v, want %q", ev.Payload["source"], config.OverlaySource)
	}
	keys, ok := ev.Payload["changedKeys"].([]any)
	if !ok || len(keys) != 2 || keys[0] != "tui.theme" {
		t.Errorf("changedKeys = %v, want the two dotted paths", ev.Payload["changedKeys"])
	}
	if _, ok := ev.Payload["timestamp"].(string); !ok {
		t.Errorf("timestamp = %v, want an RFC 3339 string", ev.Payload["timestamp"])
	}
}

func TestForwardConfigEventsCarriesTheLockList(t *testing.T) {
	sub := &subExt{baseExt: baseExt{id: "policy.acme"}, topics: []string{extension.TopicConfig}}
	mgr := managerWith(t, sub)

	config.ClearOverlayProviders()
	t.Cleanup(config.ClearOverlayProviders)
	config.RegisterOverlayProvider(config.OverlayProviderFunc(func(context.Context) (config.Overlay, error) {
		return config.Overlay{Locked: []string{"tui.theme"}}, nil
	}))
	// Register alone does not lock anything; a load does. Reaching the lock
	// list through the config package keeps this test about the bridge.
	if err := config.ApplyOverlays(context.Background()); err != nil {
		t.Skipf("configuration could not be loaded in this environment: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ForwardConfigEvents(ctx, mgr)

	config.Bus.Publish(config.ConfigChangeEvent{Event: config.EventConfigReloaded, Source: config.ReloadSource})

	if !waitFor(t, func() bool { return len(sub.seen()) == 1 }) {
		t.Fatal("config_reloaded never reached the subscriber")
	}
	ev := sub.seen()[0]
	if ev.Type != extension.EventConfigReloaded {
		t.Errorf("type = %q, want %q", ev.Type, extension.EventConfigReloaded)
	}
	locked, ok := ev.Payload["lockedKeys"].([]any)
	if !ok || len(locked) != 1 || locked[0] != "tui.theme" {
		t.Errorf("lockedKeys = %v, want [tui.theme]", ev.Payload["lockedKeys"])
	}
}

func TestForwardConfigEventsRespectsTheTopicFilter(t *testing.T) {
	wanted := &subExt{baseExt: baseExt{id: "sink.config"}, topics: []string{extension.TopicConfig}}
	other := &subExt{baseExt: baseExt{id: "sink.sessions"}, topics: []string{extension.TopicSession}}
	all := &subExt{baseExt: baseExt{id: "sink.all"}}
	mgr := managerWith(t, wanted, other, all)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ForwardConfigEvents(ctx, mgr)

	config.Bus.Publish(config.ConfigChangeEvent{Section: "tui", Source: "webui"})

	if !waitFor(t, func() bool { return len(wanted.seen()) == 1 && len(all.seen()) == 1 }) {
		t.Fatal("the config topic did not reach the subscribers that asked for it")
	}
	if got := len(other.seen()); got != 0 {
		t.Fatalf("a session-only subscriber received %d config events", got)
	}
	// An unnamed change is an ordinary update, so a subscriber that only
	// switches on Type does not need to know the config vocabulary.
	if ev := wanted.seen()[0]; ev.Type != extension.EventUpdated {
		t.Fatalf("type = %q, want %q", ev.Type, extension.EventUpdated)
	}
}

func TestForwardConfigEventsSurvivesAPanickingSubscriber(t *testing.T) {
	bad := &subExt{baseExt: baseExt{id: "sink.bad"}, topics: []string{extension.TopicConfig}, panics: true}
	good := &subExt{baseExt: baseExt{id: "sink.good"}, topics: []string{extension.TopicConfig}}
	mgr := managerWith(t, bad, good)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ForwardConfigEvents(ctx, mgr)

	config.Bus.Publish(config.ConfigChangeEvent{Event: config.EventConfigReloaded})
	config.Bus.Publish(config.ConfigChangeEvent{Event: config.EventConfigReloaded})

	if !waitFor(t, func() bool { return len(good.seen()) == 2 }) {
		t.Fatal("a panicking subscriber stopped the config fan-out")
	}
}

func TestForwardConfigEventsStopsWithItsContext(t *testing.T) {
	sub := &subExt{baseExt: baseExt{id: "sink.config"}, topics: []string{extension.TopicConfig}}
	mgr := managerWith(t, sub)

	ctx, cancel := context.WithCancel(context.Background())
	ForwardConfigEvents(ctx, mgr)
	config.Bus.Publish(config.ConfigChangeEvent{Event: config.EventConfigReloaded})
	if !waitFor(t, func() bool { return len(sub.seen()) == 1 }) {
		t.Fatal("first event never arrived")
	}

	cancel()
	// The goroutine unsubscribes on its way out; give it a moment, then check
	// that publishing is inert.
	time.Sleep(50 * time.Millisecond)
	config.Bus.Publish(config.ConfigChangeEvent{Event: config.EventConfigReloaded})
	time.Sleep(50 * time.Millisecond)
	if got := len(sub.seen()); got != 1 {
		t.Fatalf("subscriber saw %d events after cancellation, want 1", got)
	}
}
