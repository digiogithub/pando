package extensions

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// Host side of the UI policy capability.
//
// The adapter answers one question — "what should the settings surfaces show
// right now?" — by asking every loaded extension that claims to have an
// opinion, in load order, and merging the answers. Merging can only restrict:
// the hidden and read-only sets are unioned, so no provider can undo another's
// restriction, and the single-valued label and banner are taken from the first
// provider that sets them.
//
// The same containment the other capabilities get applies here: a provider that
// panics is ignored, and one that hangs is cut off, because the policy is read
// while a settings surface is being drawn and while a configuration write is
// being checked.
//
// The result also feeds the configuration write path. A path the policy hides
// is refused by config's lock check, so a client that never renders the section
// still cannot write it: hiding is presentation, and presentation is never the
// only defence.

// uiPolicyCallTimeout bounds one provider call. Like the identity capability,
// providers are contractually required to answer from state they already hold;
// the timeout is the boundary between "slow" and "wedged".
const uiPolicyCallTimeout = 2 * time.Second

// uiPolicyTTL is how long a merged policy is reused before the providers are
// asked again. It exists because the write path checks the restricted paths
// once per changed configuration path, and fanning out to extensions inside
// that loop would put extension latency on every save. A change therefore
// becomes visible within a second, which is far inside the rebuild a surface
// does when the configuration changes.
const uiPolicyTTL = time.Second

// UIPolicy is the host-side copy of extension.UIPolicy, the same duplication
// the overlay and identity capabilities make: core packages consume this type
// without importing the extension contract.
type UIPolicy struct {
	// HiddenSections lists the configuration paths no surface should render.
	HiddenSections []string
	// ReadOnlySections lists the paths rendered with their value but not
	// editable.
	ReadOnlySections []string
	// ReadOnlyLabel is the caller-supplied text shown on a read-only field.
	// Empty means the surface uses its own generic marker.
	ReadOnlyLabel string
	// Banner is the notice shown above the settings.
	Banner UIPolicyBanner
}

// UIPolicyBanner is the host-side copy of extension.UIPolicyBanner.
type UIPolicyBanner struct {
	Text string
	Link string
}

// Empty reports whether the policy asks for nothing, which is the state every
// standalone Pando is in.
func (p UIPolicy) Empty() bool {
	return len(p.HiddenSections) == 0 && len(p.ReadOnlySections) == 0 &&
		p.Banner.Text == "" && p.Banner.Link == ""
}

// IsHidden reports whether path is covered by a hidden section. Coverage is the
// same both-directions, case-insensitive segment match the lock list uses, so a
// hidden "providerAccounts" covers "providerAccounts.anthropic.apiKey".
func (p UIPolicy) IsHidden(path string) bool {
	return coversPath(p.HiddenSections, path)
}

// IsReadOnly reports whether path is covered by a read-only section. A hidden
// path is not reported as read-only: it is not rendered at all, and the
// stronger statement wins.
func (p UIPolicy) IsReadOnly(path string) bool {
	if p.IsHidden(path) {
		return false
	}
	return coversPath(p.ReadOnlySections, path)
}

// RestrictedPaths returns every path the policy withdraws from local control,
// hidden and read-only together, sorted and deduplicated. It is what the
// configuration write path refuses.
func (p UIPolicy) RestrictedPaths() []string {
	if len(p.HiddenSections) == 0 && len(p.ReadOnlySections) == 0 {
		return nil
	}
	all := make([]string, 0, len(p.HiddenSections)+len(p.ReadOnlySections))
	all = append(all, p.HiddenSections...)
	all = append(all, p.ReadOnlySections...)
	return uniqueSortedPaths(all)
}

// coversPath reports whether any of paths covers path.
func coversPath(paths []string, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, p := range paths {
		if config.PathsOverlap(p, path) {
			return true
		}
	}
	return false
}

// uniqueSortedPaths sorts paths case-insensitively and drops duplicates and
// empties, so every surface iterates the same list in the same order.
func uniqueSortedPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		if p = strings.TrimSpace(p); p != "" {
			cleaned = append(cleaned, p)
		}
	}
	sort.Slice(cleaned, func(i, j int) bool {
		return strings.ToLower(cleaned[i]) < strings.ToLower(cleaned[j])
	})
	out := make([]string, 0, len(cleaned))
	for _, p := range cleaned {
		if len(out) > 0 && strings.EqualFold(p, out[len(out)-1]) {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// UIPolicyResolver returns a function reporting the merged policy of every
// loaded provider. The returned function is safe for concurrent use and always
// non-nil, so callers need no nil check; with no provider loaded it always
// returns the zero policy and every surface behaves exactly as an unextended
// Pando.
func UIPolicyResolver(mgr *extension.Manager) func(ctx context.Context) UIPolicy {
	providers := extension.Capability[extension.UIPolicyProvider](mgr)
	if len(providers) == 0 {
		return func(context.Context) UIPolicy { return UIPolicy{} }
	}
	return func(ctx context.Context) UIPolicy {
		return mergeUIPolicies(ctx, providers)
	}
}

// mergeUIPolicies asks every provider in load order and combines the answers.
//
// Hidden and read-only sections are unioned: a policy is a restriction, so
// merging two of them can only restrict further, and no provider can lift
// another's restriction. The label and the banner are single-valued, so the
// first provider in load order that sets one wins and a later one is dropped
// with a log line; there is no way to show two banners in one row, and picking
// the first keeps the outcome deterministic instead of load-order-sensitive in
// a way nobody can predict.
func mergeUIPolicies(ctx context.Context, providers []extension.UIPolicyProvider) UIPolicy {
	var merged UIPolicy
	var hidden, readOnly []string
	labelFrom, bannerFrom := "", ""

	for _, p := range providers {
		got, ok := callUIPolicyProvider(ctx, p)
		if !ok {
			continue
		}
		owner := ownerName(p.ExtensionInfo().ID)
		hidden = append(hidden, got.HiddenSections...)
		readOnly = append(readOnly, got.ReadOnlySections...)

		if label := strings.TrimSpace(got.ReadOnlyLabel); label != "" {
			if labelFrom == "" {
				merged.ReadOnlyLabel, labelFrom = label, owner
			} else if label != merged.ReadOnlyLabel {
				logging.Debug("Ignoring a second UI policy read-only label",
					"extension", owner, "kept_from", labelFrom)
			}
		}
		if got.Banner.Text != "" || got.Banner.Link != "" {
			if bannerFrom == "" {
				merged.Banner = UIPolicyBanner{Text: got.Banner.Text, Link: got.Banner.Link}
				bannerFrom = owner
			} else {
				logging.Warn("Ignoring a second UI policy banner, only one is shown",
					"extension", owner, "kept_from", bannerFrom)
			}
		}
	}

	merged.HiddenSections = uniqueSortedPaths(hidden)
	merged.ReadOnlySections = uniqueSortedPaths(readOnly)
	return merged
}

// callUIPolicyProvider runs one provider under a deadline, converting a panic
// into "no policy" so that an optional capability cannot break the settings
// surface that consults it.
func callUIPolicyProvider(parent context.Context, p extension.UIPolicyProvider) (policy UIPolicy, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension call panicked, ignoring its result",
				"extension", ownerName(p.ExtensionInfo().ID), "call", "UIPolicy", "panic", r)
			policy, ok = UIPolicy{}, false
		}
	}()

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, uiPolicyCallTimeout)
	defer cancel()

	got, found := p.UIPolicy(ctx)
	if !found {
		return UIPolicy{}, false
	}
	return UIPolicy{
		HiddenSections:   append([]string(nil), got.HiddenSections...),
		ReadOnlySections: append([]string(nil), got.ReadOnlySections...),
		ReadOnlyLabel:    got.ReadOnlyLabel,
		Banner:           UIPolicyBanner{Text: got.Banner.Text, Link: got.Banner.Link},
	}, true
}

// The process-wide policy. Surfaces that cannot reach the extension manager —
// an HTTP handler, a settings page deep in the TUI — read it from here, and the
// configuration write path takes its restricted paths from the same value, so
// what is hidden and what is refused can never disagree.
var (
	uiPolicyMu        sync.Mutex
	uiPolicyResolver  func(ctx context.Context) UIPolicy
	uiPolicyCached    UIPolicy
	uiPolicyCachedAt  time.Time
	uiPolicyResolving bool
)

// RegisterUIPolicy installs the merged policy of mgr as the process-wide one
// and points the configuration write path at it, so a hidden or read-only path
// is refused as well as not offered.
//
// It is safe to call with a manager that has no provider: that installs the
// empty policy, which is what an unextended Pando has.
func RegisterUIPolicy(mgr *extension.Manager) {
	SetUIPolicyResolver(UIPolicyResolver(mgr))
}

// SetUIPolicyResolver replaces the process-wide resolver. Passing nil removes
// it, restoring the unextended behaviour; that is what a host tearing a manager
// down does, and what a test does when it is finished.
func SetUIPolicyResolver(fn func(ctx context.Context) UIPolicy) {
	uiPolicyMu.Lock()
	uiPolicyResolver = fn
	uiPolicyCached, uiPolicyCachedAt = UIPolicy{}, time.Time{}
	uiPolicyMu.Unlock()

	if fn == nil {
		config.SetRestrictedPathSource(nil)
		return
	}
	config.SetRestrictedPathSource(func() []string {
		return CurrentUIPolicy(context.Background()).RestrictedPaths()
	})
}

// InvalidateUIPolicy drops the memoised policy so the next read asks the
// providers again. Call it when something is known to have changed the policy
// and the surface cannot wait for the memo to expire.
func InvalidateUIPolicy() {
	uiPolicyMu.Lock()
	uiPolicyCached, uiPolicyCachedAt = UIPolicy{}, time.Time{}
	uiPolicyMu.Unlock()
}

// CurrentUIPolicy returns the process-wide merged policy, memoised for
// uiPolicyTTL. With no resolver installed it returns the zero policy.
//
// The providers are asked outside the lock, and a call that arrives while
// another is resolving gets the previous value rather than starting a second
// resolution. That is what makes the call re-entrant: a provider that derives
// its policy from the configuration reaches the lock check, which reads the
// restricted paths, which lands back here, and must find an answer instead of
// its own call.
func CurrentUIPolicy(ctx context.Context) UIPolicy {
	uiPolicyMu.Lock()
	fn := uiPolicyResolver
	fresh := !uiPolicyCachedAt.IsZero() && time.Since(uiPolicyCachedAt) < uiPolicyTTL
	if fn == nil || fresh || uiPolicyResolving {
		cached := uiPolicyCached
		uiPolicyMu.Unlock()
		if fn == nil {
			return UIPolicy{}
		}
		return cached
	}
	uiPolicyResolving = true
	uiPolicyMu.Unlock()

	// The flag is cleared even if the resolver panics, so one bad call cannot
	// freeze the policy at its previous value for the life of the process.
	defer func() {
		uiPolicyMu.Lock()
		uiPolicyResolving = false
		uiPolicyMu.Unlock()
	}()

	policy := fn(ctx)

	uiPolicyMu.Lock()
	uiPolicyCached, uiPolicyCachedAt = policy, time.Now()
	uiPolicyMu.Unlock()
	return policy
}
