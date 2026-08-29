package events

import (
	"context"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Subscriber is implemented by a core.Backend that can push live UI events
// for a scope instead of being polled. It is intentionally a separate,
// optional interface (like core.Backend's own PhysicalInput/screen split)
// so a backend that cannot genuinely support it simply does not implement
// it -- callers detect support via a type assertion
// (backend.(events.Subscriber)) and fall back to polling, never a runtime
// panic or a faked capability.
//
// Subscribe returns a channel of events (closed when the subscription
// ends, e.g. the backend connection drops) and an idempotent unsubscribe
// func. Implementations must never block the caller of Subscribe itself on
// waiting for an actual event; event delivery afterwards is
// best-effort/buffered (a slow consumer may miss events -- WaitFor always
// re-checks the real condition, so a missed event only costs a slightly
// later re-check).
type Subscriber interface {
	Subscribe(ctx context.Context, scope core.Scope) (<-chan Event, func(), error)
}
