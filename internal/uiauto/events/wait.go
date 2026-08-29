package events

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// eventFallbackPollInterval is how often WaitFor re-checks the condition
// on its own, in addition to reacting to received events, while a live
// subscription is active. This is deliberately much coarser than
// core.WaitFor's 200ms default poll interval -- events should normally
// wake the waiter well before this fires -- but it is what keeps "keep
// polling as the fallback" true even for a change the backend does not
// (or cannot) model as one of this package's Kinds, or an event dropped
// by EventBus's best-effort delivery.
const eventFallbackPollInterval = 2 * time.Second

// evaluate resolves l.Selector within l.Scope against backend and reports
// whether cond is satisfied, mirroring core.Locator's own (unexported)
// evaluate semantics using only backend.Find directly: ELEMENT_NOT_FOUND
// is treated as "not yet found" for every condition except
// ConditionNotExists; any other error is propagated.
func evaluate(ctx context.Context, backend core.Backend, l *core.Locator, cond core.Condition) (bool, *core.Element, error) {
	matches, err := backend.Find(ctx, l.Scope, l.Selector, 1)
	if err != nil {
		if de, ok := core.AsDesktopError(err); ok && de.Code == core.ErrElementNotFound {
			matches = nil
		} else {
			return false, nil, err
		}
	}
	found := len(matches) > 0

	switch cond {
	case core.ConditionNotExists:
		return !found, nil, nil
	case core.ConditionExists:
		if !found {
			return false, nil, nil
		}
		return true, matches[0], nil
	case core.ConditionVisible:
		if !found {
			return false, nil, nil
		}
		return matches[0].Visible, matches[0], nil
	case core.ConditionEnabled:
		if !found {
			return false, nil, nil
		}
		return matches[0].Enabled, matches[0], nil
	case core.ConditionFocused:
		if !found {
			return false, nil, nil
		}
		return matches[0].Focused, matches[0], nil
	default:
		return false, nil, core.NewInvalidArgsError(string(cond) + ": unknown wait condition")
	}
}

// WaitFor waits for cond to hold for l against backend, preferring a live
// subscription from sub (when non-nil) and transparently falling back to
// core.WaitFor's polling loop when sub is nil, sub.Subscribe itself fails,
// or the subscription channel closes before the condition is met (the
// polling loop then covers the rest of the deadline). Even on the
// event-driven path, every received event only triggers a fresh,
// authoritative evaluate() call against backend -- an event is a wake-up
// signal, never trusted as the source of truth by itself.
func WaitFor(ctx context.Context, backend core.Backend, sub Subscriber, l *core.Locator, cond core.Condition, timeout time.Duration) (*core.Element, error) {
	// Always check immediately first, exactly like core.WaitFor.
	if ok, el, err := evaluate(ctx, backend, l, cond); err != nil {
		return nil, err
	} else if ok {
		return el, nil
	}
	if timeout <= 0 {
		return nil, core.NewTimeoutError("condition \"" + string(cond) + "\" was not met immediately and timeout is zero")
	}

	if sub == nil {
		return core.WaitFor(ctx, backend, l, cond, timeout, 0)
	}

	deadline := time.Now().Add(timeout)
	ch, unsub, err := sub.Subscribe(ctx, l.Scope)
	if err != nil || ch == nil {
		// Subscription itself failed: fall back to polling for the
		// remaining deadline, honestly and silently -- Capabilities.Events
		// already told the caller whether to expect a live path.
		return core.WaitFor(ctx, backend, l, cond, time.Until(deadline), 0)
	}
	defer unsub()

	ticker := time.NewTicker(eventFallbackPollInterval)
	defer ticker.Stop()

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, core.NewTimeoutError("condition \"" + string(cond) + "\" was not met within " + timeout.String() + " (event-driven wait)")
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
			return nil, core.NewTimeoutError("condition \"" + string(cond) + "\" was not met within " + timeout.String() + " (event-driven wait)")
		case _, ok := <-ch:
			timer.Stop()
			if !ok {
				// Subscription ended early: finish out the deadline by
				// polling instead of failing the whole wait.
				return core.WaitFor(ctx, backend, l, cond, time.Until(deadline), 0)
			}
			if satisfied, el, evalErr := evaluate(ctx, backend, l, cond); evalErr != nil {
				return nil, evalErr
			} else if satisfied {
				return el, nil
			}
		case <-ticker.C:
			timer.Stop()
			if satisfied, el, evalErr := evaluate(ctx, backend, l, cond); evalErr != nil {
				return nil, evalErr
			} else if satisfied {
				return el, nil
			}
		}
	}
}
