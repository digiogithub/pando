package extensions

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/pkg/extension"
)

// Configuration changes as an extension event topic.
//
// The configuration bus is older than the extension system and is generic over
// nothing: it carries one struct, to in-process subscribers, on a best-effort
// basis. This file bridges it to the public contract so an extension sees
// configuration changes through the same EventSubscriber interface it already
// uses for sessions and messages, instead of importing internal/config.
//
// Why it matters for an extension that imposes configuration: after it asks
// for a reload it needs to know that the reload happened, which keys moved and
// what the lock list is now. Reading it back through ConfigView would race the
// load; the event arrives when the values are already in effect.

// configEventBuffer is how many configuration events the bridge holds while a
// slow subscriber is being called. The bus itself drops rather than blocks, so
// this only smooths a burst (a settings page saving several sections); it is
// not a queue anything can rely on.
const configEventBuffer = 32

// ForwardConfigEvents delivers configuration changes to every EventSubscriber
// that asked for extension.TopicConfig, until ctx is cancelled. It returns
// immediately; the fan-out runs in its own goroutine, and nothing at all is
// started when no extension subscribes.
func ForwardConfigEvents(ctx context.Context, mgr *extension.Manager) {
	if mgr == nil || !HasEventSubscribers(mgr) {
		return
	}

	ch := make(chan config.ConfigChangeEvent, configEventBuffer)
	config.Bus.Subscribe(ch)

	go func() {
		defer config.Bus.Unsubscribe(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				dispatch(ctx, mgr, configEvent(ev))
			}
		}
	}()
}

// configEvent renders one bus event in the public shape. The payload is built
// by hand rather than by marshalling the internal struct, so the field names
// extensions see are a contract of this package and not a consequence of a
// struct tag someone may rename.
func configEvent(ev config.ConfigChangeEvent) extension.Event {
	payload := map[string]any{
		"event":   ev.Event,
		"section": ev.Section,
		"source":  ev.Source,
	}
	if len(ev.ChangedKeys) > 0 {
		payload["changedKeys"] = toAnySlice(ev.ChangedKeys)
	}
	if locked := config.LockedKeys(); len(locked) > 0 {
		payload["lockedKeys"] = toAnySlice(locked)
	}
	when := ev.Timestamp
	if when.IsZero() {
		when = time.Now()
	}
	payload["timestamp"] = when.Format(time.RFC3339Nano)

	return extension.Event{
		Topic:   extension.TopicConfig,
		Type:    configEventType(ev.Event),
		Payload: payload,
		Time:    when,
	}
}

// configEventType maps the bus's event name to the public type. An unnamed
// change is an update: a subscriber that only cares that something moved can
// then treat every topic the same way.
func configEventType(name string) extension.EventType {
	switch name {
	case config.EventOverlayApplied:
		return extension.EventOverlayApplied
	case config.EventConfigReloaded:
		return extension.EventConfigReloaded
	case "":
		return extension.EventUpdated
	default:
		return extension.EventType(name)
	}
}

// toAnySlice converts a string list to the []any a JSON-shaped payload uses,
// so a subscriber decodes every payload value the same way.
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
