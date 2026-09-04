package extensions

import (
	"context"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host side of the identity capability.
//
// The adapter answers one question — "who is the user right now?" — by asking
// every loaded extension that claims to know, in load order, and taking the
// first answer. It is called per use rather than once at start-up, so a
// sign-in or a sign-out during a run is picked up on the next event.
//
// Two properties matter here and are enforced rather than assumed. A provider
// that panics is contained and treated as "does not know", because an optional
// capability must not be able to break a memory write. And a provider that
// hangs is cut off, because this call sits on the synchronous path of a
// knowledge-base write: the contract says answer from state you already hold,
// and the timeout is what makes the contract binding.

// identityCallTimeout bounds one provider call. It is a boundary between
// "slow" and "wedged", not a tuning knob: providers are contractually required
// to answer from memory.
const identityCallTimeout = 2 * time.Second

// Identity is the host-side copy of extension.Identity. The duplication is the
// same one internal/config makes for overlays: core packages consume this
// type without importing the extension contract, and the contract stays free
// of internal types.
type Identity struct {
	UserID   string
	Email    string
	DeviceID string
	Groups   []string
}

// IdentityResolver returns a function that reports the current identity, or
// false when nothing knows one. The returned function is safe for concurrent
// use and always non-nil, so callers need no nil check; with no provider
// loaded it simply always returns false and the host behaves exactly as an
// unextended Pando.
func IdentityResolver(mgr *extension.Manager) func(ctx context.Context) (Identity, bool) {
	providers := extension.Capability[extension.IdentityProvider](mgr)
	if len(providers) == 0 {
		return func(context.Context) (Identity, bool) { return Identity{}, false }
	}
	return func(ctx context.Context) (Identity, bool) {
		for _, p := range providers {
			if id, ok := callIdentityProvider(ctx, p); ok {
				return id, true
			}
		}
		return Identity{}, false
	}
}

// callIdentityProvider runs one provider under a deadline, converting a panic
// into "does not know".
func callIdentityProvider(parent context.Context, p extension.IdentityProvider) (id Identity, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension call panicked, ignoring its result",
				"extension", ownerName(p.ExtensionInfo().ID), "call", "Identity", "panic", r)
			id, ok = Identity{}, false
		}
	}()

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, identityCallTimeout)
	defer cancel()

	got, found := p.Identity(ctx)
	if !found {
		return Identity{}, false
	}
	return Identity{
		UserID:   got.UserID,
		Email:    got.Email,
		DeviceID: got.DeviceID,
		Groups:   append([]string(nil), got.Groups...),
	}, true
}
