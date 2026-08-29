package core

import (
	"context"
	"fmt"
	"time"
)

// Locator is a lazy reference to an element: it captures a Scope and
// Selector and only resolves against a Backend when asked to, so callers
// can re-resolve immediately before acting instead of trusting a
// potentially stale snapshot.
type Locator struct {
	Scope    Scope
	Selector *Selector
}

// NewLocator builds a Locator.
func NewLocator(scope Scope, sel *Selector) *Locator {
	return &Locator{Scope: scope, Selector: sel}
}

// Resolve resolves the locator against b, returning the single best match.
// It returns an ELEMENT_NOT_FOUND DesktopError when nothing matches.
func (l *Locator) Resolve(ctx context.Context, b Backend) (*Element, error) {
	matches, err := l.resolveAllLimit(ctx, b, 1)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, NewElementNotFoundError(fmt.Sprintf("no element matched selector %q", l.Selector.String()))
	}
	return matches[0], nil
}

// ResolveAll resolves the locator against b, returning up to limit matches
// (limit <= 0 means "no explicit cap", left to the backend's default).
func (l *Locator) ResolveAll(ctx context.Context, b Backend) ([]*Element, error) {
	return b.Find(ctx, l.Scope, l.Selector, 0)
}

// resolveAllLimit is an internal helper for Resolve's single-match fast
// path.
func (l *Locator) resolveAllLimit(ctx context.Context, b Backend, limit int) ([]*Element, error) {
	return b.Find(ctx, l.Scope, l.Selector, limit)
}

// Condition is a wait predicate evaluated by WaitFor against the element(s)
// a Locator resolves to.
type Condition string

const (
	// ConditionExists waits until the locator resolves to at least one
	// element.
	ConditionExists Condition = "exists"
	// ConditionNotExists waits until the locator resolves to no element.
	ConditionNotExists Condition = "notexists"
	// ConditionVisible waits until the locator resolves to an element
	// with Visible == true.
	ConditionVisible Condition = "visible"
	// ConditionEnabled waits until the locator resolves to an element
	// with Enabled == true.
	ConditionEnabled Condition = "enabled"
	// ConditionFocused waits until the locator resolves to an element
	// with Focused == true.
	ConditionFocused Condition = "focused"
)

// evaluate resolves l and checks cond against the result, returning
// (satisfied, element-or-nil, error). A resolution error other than "not
// found" is propagated; "not found" is treated as "condition not yet met"
// for every condition except ConditionNotExists.
func (l *Locator) evaluate(ctx context.Context, b Backend, cond Condition) (bool, *Element, error) {
	matches, err := l.resolveAllLimit(ctx, b, 1)
	if err != nil {
		if de, ok := AsDesktopError(err); ok && de.Code == ErrElementNotFound {
			matches = nil
		} else {
			return false, nil, err
		}
	}
	found := len(matches) > 0

	switch cond {
	case ConditionNotExists:
		return !found, nil, nil
	case ConditionExists:
		if !found {
			return false, nil, nil
		}
		return true, matches[0], nil
	case ConditionVisible:
		if !found {
			return false, nil, nil
		}
		return matches[0].Visible, matches[0], nil
	case ConditionEnabled:
		if !found {
			return false, nil, nil
		}
		return matches[0].Enabled, matches[0], nil
	case ConditionFocused:
		if !found {
			return false, nil, nil
		}
		return matches[0].Focused, matches[0], nil
	default:
		return false, nil, NewInvalidArgsError(fmt.Sprintf("unknown wait condition %q", cond))
	}
}

// WaitFor polls the locator against b until cond is satisfied, timeout
// elapses or ctx is cancelled, checking every interval. It always performs
// at least one immediate check before waiting. On success it returns the
// matched element (nil for ConditionNotExists). On timeout it returns a
// TIMEOUT DesktopError; on context cancellation it returns ctx.Err().
func WaitFor(ctx context.Context, b Backend, l *Locator, cond Condition, timeout, interval time.Duration) (*Element, error) {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	if timeout <= 0 {
		deadline = time.Now()
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ok, el, err := l.evaluate(ctx, b, cond)
		if err != nil {
			return nil, err
		}
		if ok {
			return el, nil
		}
		if timeout <= 0 || !time.Now().Before(deadline) {
			return nil, NewTimeoutError(fmt.Sprintf("condition %q was not met for selector %q within %s", cond, l.Selector.String(), timeout))
		}

		remaining := time.Until(deadline)
		wait := interval
		if remaining < wait {
			wait = remaining
		}
		if wait <= 0 {
			return nil, NewTimeoutError(fmt.Sprintf("condition %q was not met for selector %q within %s", cond, l.Selector.String(), timeout))
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
