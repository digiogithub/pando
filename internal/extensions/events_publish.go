package extensions

import (
	"context"

	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host-published topics: the events core raises itself rather than reads off a
// resource broker.
//
// Sessions, messages and permissions are brokered resources, so Forward can
// subscribe to them generically. A tool call, an MCP handshake, a skill
// activation and a provider account edit are not resources and have no broker:
// they happen inside the code that performs them. internal/extevents is the
// publishing end they call, and this file is the receiving end that turns what
// they publish into subscriber calls, with the same panic containment and the
// same per-call deadline every other capability gets.

// StartEventPublisher routes events published through internal/extevents to
// the loaded subscribers until ctx is cancelled. Nothing is installed when no
// extension subscribes, which leaves every publish site in core at one atomic
// load.
func StartEventPublisher(ctx context.Context, mgr *extension.Manager) {
	if mgr == nil || !HasEventSubscribers(mgr) {
		return
	}
	extevents.SetSink(ctx, func(ev extension.Event) {
		dispatch(ctx, mgr, ev)
	})
}
