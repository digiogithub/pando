// Package extevents is the publishing end of the extension event stream.
//
// The problem it solves is a dependency one. Events have to be published where
// they happen: a tool call from the agent loop, an MCP handshake from the MCP
// client, a provider account edit from the configuration package. Those
// packages cannot import internal/extensions, which is the host side of the
// extension system and already imports several of them. So the publishing end
// is this leaf package, which imports nothing but the public contract, and
// internal/extensions installs itself as the sink at start-up.
//
// Two properties are guaranteed to callers, because a publish sits inside the
// operation it reports:
//
//   - Publishing never blocks. Events go to a bounded channel and are dropped
//     when it is full, exactly as the configuration bus already drops. An
//     event stream is accounting, and accounting must not be able to stall the
//     work it is accounting for.
//   - Publishing costs one atomic load when nothing subscribes, which is every
//     standard build: no sink is installed, so Publish returns immediately and
//     the helpers below do not even build their payload.
package extevents

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// bufferSize is how many published events are held while the sink is busy.
// It smooths a burst (a tool loop, a fleet of MCP servers starting together);
// it is not a queue anything may rely on, because a full buffer drops.
const bufferSize = 256

// Sink consumes published events. It is called from the package's own delivery
// goroutine, one event at a time, never from the publisher's goroutine.
type Sink func(ev extension.Event)

// stream is the delivery channel, nil-valued when no sink is installed. It is
// read atomically on every publish, so installing or removing a sink races
// with a publish without a lock on the hot path.
var (
	stream  atomic.Pointer[chan extension.Event]
	sinkMu  sync.Mutex
	sinkGen uint64
)

// SetSink installs sink as the consumer of published events and starts the
// goroutine that feeds it, stopping when ctx is cancelled. A nil sink detaches
// the previous one, which is what tests and teardown use.
//
// Calling it again replaces the sink: the previous delivery goroutine drains
// nothing further and exits.
func SetSink(ctx context.Context, sink Sink) {
	sinkMu.Lock()
	defer sinkMu.Unlock()

	sinkGen++
	if sink == nil {
		stream.Store(nil)
		return
	}

	ch := make(chan extension.Event, bufferSize)
	stream.Store(&ch)

	go func() {
		for {
			select {
			case <-ctx.Done():
				sinkMu.Lock()
				if cur := stream.Load(); cur != nil && *cur == ch {
					stream.Store(nil)
				}
				sinkMu.Unlock()
				return
			case ev := <-ch:
				sink(ev)
			}
		}
	}()
}

// Enabled reports whether anything is listening. Call sites that would have to
// do real work to build a payload check it first; the helpers in this package
// already do.
func Enabled() bool {
	return stream.Load() != nil
}

// Publish hands one event to the installed sink. It never blocks and never
// returns an error: with no sink it does nothing, and with a full buffer the
// event is dropped and counted in the debug log.
func Publish(ev extension.Event) {
	ch := stream.Load()
	if ch == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	select {
	case *ch <- ev:
	default:
		logging.Debug("Extension event dropped, publish buffer is full",
			"topic", ev.Topic, "type", string(ev.Type))
	}
}

// ToolStarted reports that the host handed a tool call to its tool.
func ToolStarted(sessionID, callID, name, agent string) {
	if !Enabled() {
		return
	}
	Publish(extension.Event{
		Topic:     extension.TopicTool,
		Type:      extension.EventStarted,
		ID:        callID,
		SessionID: sessionID,
		Payload: map[string]any{
			"name":  name,
			"agent": agent,
		},
	})
}

// ToolCompleted reports that a tool call finished. A nil err means success;
// the error text is published because it is the host's own diagnosis of the
// call, not user content.
func ToolCompleted(sessionID, callID, name, agent string, dur time.Duration, err error) {
	if !Enabled() {
		return
	}
	payload := map[string]any{
		"name":       name,
		"agent":      agent,
		"durationMs": float64(dur.Milliseconds()),
		"ok":         err == nil,
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	Publish(extension.Event{
		Topic:     extension.TopicTool,
		Type:      extension.EventCompleted,
		ID:        callID,
		SessionID: sessionID,
		Payload:   payload,
	})
}

// MCPHandshake reports the outcome of connecting to an MCP server. authNeeded
// distinguishes "this server would work once somebody signs in" from a plain
// failure, because the two ask for very different things from an operator.
func MCPHandshake(server, transport, client string, err error, authNeeded bool) {
	if !Enabled() {
		return
	}
	evType := extension.EventConnected
	switch {
	case authNeeded:
		evType = extension.EventAuthRequired
	case err != nil:
		evType = extension.EventFailed
	}
	payload := map[string]any{
		"server":    server,
		"transport": transport,
		"client":    client,
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	Publish(extension.Event{
		Topic:   extension.TopicMCP,
		Type:    evType,
		ID:      server,
		Payload: payload,
	})
}

// SkillActivated reports that a skill was switched on for a run.
func SkillActivated(sessionID, name, agent, source string) {
	if !Enabled() {
		return
	}
	Publish(extension.Event{
		Topic:     extension.TopicSkill,
		Type:      extension.EventActivated,
		ID:        name,
		SessionID: sessionID,
		Payload: map[string]any{
			"name":   name,
			"agent":  agent,
			"source": source,
		},
	})
}

// ProviderAccountAdded reports a newly configured provider account.
func ProviderAccountAdded(id, providerType string, disabled bool) {
	providerAccountChanged(extension.EventCreated, id, providerType, disabled)
}

// ProviderAccountUpdated reports an edited provider account, including one
// that was only enabled or disabled.
func ProviderAccountUpdated(id, providerType string, disabled bool) {
	providerAccountChanged(extension.EventUpdated, id, providerType, disabled)
}

// ProviderAccountRemoved reports a deleted provider account.
func ProviderAccountRemoved(id, providerType string) {
	providerAccountChanged(extension.EventDeleted, id, providerType, false)
}

// providerAccountChanged publishes one provider account change. Only the
// identity of the account travels: never a key, a token or a base URL, because
// this topic leaves the process in some deployments.
func providerAccountChanged(evType extension.EventType, id, providerType string, disabled bool) {
	if !Enabled() {
		return
	}
	Publish(extension.Event{
		Topic: extension.TopicProvider,
		Type:  evType,
		ID:    id,
		Payload: map[string]any{
			"id":           id,
			"providerType": providerType,
			"disabled":     disabled,
		},
	})
}
