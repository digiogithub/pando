package extensions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/pkg/extension"
)

// Core's brokers are generic over internal types (session.Session,
// message.Message, ...), which an out-of-tree extension cannot name. Forward
// bridges the two: it subscribes to a typed broker and hands subscribers the
// resource in its JSON form — the same shape the REST API already exposes, so
// nothing internal leaks into the public contract.

// HasEventSubscribers reports whether anything would consume forwarded events.
// Callers use it to avoid starting fan-out goroutines in a standard build.
func HasEventSubscribers(mgr *extension.Manager) bool {
	if mgr == nil {
		return false
	}
	return len(extension.Capability[extension.EventSubscriber](mgr)) > 0
}

// Forward subscribes to src and delivers its events to every EventSubscriber
// that asked for topic, until ctx is cancelled. It returns immediately; the
// fan-out runs in its own goroutine.
//
// Events are dropped, never queued, when a subscriber is slow: the alternative
// is unbounded memory growth in the host because an extension misbehaves. The
// contract says so, and an extension that must not lose events buffers them.
func Forward[T any](ctx context.Context, mgr *extension.Manager, topic string, src pubsub.Suscriber[T]) {
	if mgr == nil || src == nil || !HasEventSubscribers(mgr) {
		return
	}
	ch := src.Subscribe(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				deliver(ctx, mgr, topic, ev)
			}
		}
	}()
}

// deliver converts one typed event and hands it to the interested subscribers.
func deliver[T any](ctx context.Context, mgr *extension.Manager, topic string, ev pubsub.Event[T]) {
	payload, err := toPayload(ev.Payload)
	if err != nil {
		logging.Debug("Extension event dropped, payload is not JSON-encodable",
			"topic", topic, "error", err)
		return
	}
	out := extension.Event{
		Topic:     topic,
		Type:      extension.EventType(ev.Type),
		ID:        stringField(payload, "id", "ID"),
		SessionID: stringField(payload, "sessionId", "session_id", "sessionID", "SessionID"),
		Payload:   payload,
		Time:      time.Now(),
	}
	dispatch(ctx, mgr, out)
}

// dispatch hands one ready-made event to every subscriber that asked for its
// topic. Delivery is sequential and guarded; see handleSafely.
func dispatch(ctx context.Context, mgr *extension.Manager, ev extension.Event) {
	for _, sub := range extension.Capability[extension.EventSubscriber](mgr) {
		if !wants(sub, ev.Topic) {
			continue
		}
		handleSafely(ctx, sub, ev)
	}
}

// wants reports whether sub asked for this topic. An empty topic list means
// every topic, including ones added after the extension was written.
func wants(sub extension.EventSubscriber, topic string) bool {
	topics, ok := guardValue("EventSubscriber.Topics", sub.ExtensionInfo().ID, sub.Topics)
	if !ok {
		// A subscriber that cannot say what it wants gets nothing: guessing
		// "everything" would hand a broken extension the whole event stream.
		return false
	}
	if len(topics) == 0 {
		return true
	}
	for _, t := range topics {
		if t == topic {
			return true
		}
	}
	return false
}

// handleSafely contains a panicking or wedged subscriber: it must not kill the
// fan-out goroutine, and with it every other subscriber's events.
//
// The deadline matters as much as the panic guard here. Delivery is sequential,
// so one subscriber blocking forever silences every subscriber after it and
// backs the event stream up behind it. A handler with real work to do must
// start it and return.
func handleSafely(ctx context.Context, sub extension.EventSubscriber, ev extension.Event) {
	id := sub.ExtensionInfo().ID
	guardDeclarative(ctx, "EventSubscriber.HandleEvent", id, func(callCtx context.Context) struct{} {
		sub.HandleEvent(callCtx, ev)
		return struct{}{}
	})
}

// toPayload renders a resource as JSON-decoded values. Marshalling is what
// keeps internal types out of the contract, and it is also what guarantees the
// subscriber cannot mutate host state through the payload.
func toPayload(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any)
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// stringField returns the first of names present in payload as a string.
func stringField(payload map[string]any, names ...string) string {
	for _, name := range names {
		if s, ok := payload[name].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
