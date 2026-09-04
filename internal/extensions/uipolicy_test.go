package extensions

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/pkg/extension"
)

type uiPolicyExt struct {
	baseExt
	policy extension.UIPolicy
	has    bool
	panics bool
	block  bool

	calls int
}

func (e *uiPolicyExt) ExtensionInfo() extension.Info { return e.info(e) }

func (e *uiPolicyExt) UIPolicy(ctx context.Context) (extension.UIPolicy, bool) {
	e.calls++
	if e.panics {
		panic("boom")
	}
	if e.block {
		<-ctx.Done()
		return extension.UIPolicy{}, false
	}
	return e.policy, e.has
}

// A build with no policy provider must behave exactly as it did before the
// capability existed: nothing hidden, nothing read-only, no banner.
func TestUIPolicyResolverWithoutProvider(t *testing.T) {
	resolve := UIPolicyResolver(managerWith(t))
	if resolve == nil {
		t.Fatal("resolver is nil, callers would have to nil-check it")
	}
	policy := resolve(context.Background())
	if !policy.Empty() || policy.IsHidden("tui.theme") || policy.IsReadOnly("tui.theme") {
		t.Fatalf("got %+v with no provider loaded", policy)
	}
	if UIPolicyResolver(nil)(context.Background()).Empty() != true {
		t.Fatal("a nil manager produced a policy")
	}
}

// Two providers are merged by restriction: the sets are unioned so neither can
// undo the other, while the single-valued label and banner are taken from the
// first provider in load order that sets them.
func TestUIPolicyMergesTwoProviders(t *testing.T) {
	first := &uiPolicyExt{
		baseExt: baseExt{id: "policy.a"},
		has:     true,
		policy: extension.UIPolicy{
			HiddenSections:   []string{"providerAccounts", "internalTools"},
			ReadOnlySections: []string{"tui.theme"},
			ReadOnlyLabel:    "Managed by the operator",
			Banner:           extension.UIPolicyBanner{Text: "This device is managed", Link: "https://example.test/a"},
		},
	}
	second := &uiPolicyExt{
		baseExt: baseExt{id: "policy.b"},
		has:     true,
		policy: extension.UIPolicy{
			HiddenSections:   []string{"internalTools", "mcpServers"},
			ReadOnlySections: []string{"agents"},
			ReadOnlyLabel:    "Managed elsewhere",
			Banner:           extension.UIPolicyBanner{Text: "Second banner"},
		},
	}
	silent := &uiPolicyExt{baseExt: baseExt{id: "policy.c"}}

	policy := UIPolicyResolver(managerWith(t, first, second, silent))(context.Background())

	wantHidden := []string{"internalTools", "mcpServers", "providerAccounts"}
	if !reflect.DeepEqual(policy.HiddenSections, wantHidden) {
		t.Fatalf("hidden = %v, want %v (union, sorted, deduplicated)", policy.HiddenSections, wantHidden)
	}
	wantReadOnly := []string{"agents", "tui.theme"}
	if !reflect.DeepEqual(policy.ReadOnlySections, wantReadOnly) {
		t.Fatalf("read-only = %v, want %v", policy.ReadOnlySections, wantReadOnly)
	}
	if policy.ReadOnlyLabel != "Managed by the operator" {
		t.Fatalf("label = %q, want the first provider's", policy.ReadOnlyLabel)
	}
	if policy.Banner.Text != "This device is managed" || policy.Banner.Link != "https://example.test/a" {
		t.Fatalf("banner = %+v, want the first provider's whole banner", policy.Banner)
	}
	if silent.calls != 1 {
		t.Fatalf("a provider that has nothing to say was consulted %d times, want 1", silent.calls)
	}

	// Coverage runs the way the lock list runs it: a hidden section covers what
	// is under it, and hiding beats read-only for a path in both lists.
	if !policy.IsHidden("providerAccounts.anthropic.apiKey") {
		t.Fatal("a hidden section did not cover a key under it")
	}
	if !policy.IsReadOnly("TUI.Theme") {
		t.Fatal("read-only matching is not case-insensitive")
	}
	if policy.IsReadOnly("tui.nerdFonts") {
		t.Fatal("a sibling key was reported read-only")
	}
	wantRestricted := []string{"agents", "internalTools", "mcpServers", "providerAccounts", "tui.theme"}
	if !reflect.DeepEqual(policy.RestrictedPaths(), wantRestricted) {
		t.Fatalf("restricted = %v, want %v", policy.RestrictedPaths(), wantRestricted)
	}
}

// A provider that panics is contained and contributes nothing, and the
// providers after it are still asked: an optional capability must not be able
// to take the settings surface down with it.
func TestUIPolicyContainsPanicAndTimeout(t *testing.T) {
	bad := &uiPolicyExt{baseExt: baseExt{id: "policy.panics"}, panics: true}
	good := &uiPolicyExt{
		baseExt: baseExt{id: "policy.good"},
		has:     true,
		policy:  extension.UIPolicy{HiddenSections: []string{"mcpServers"}},
	}

	policy := UIPolicyResolver(managerWith(t, bad, good))(context.Background())
	if !reflect.DeepEqual(policy.HiddenSections, []string{"mcpServers"}) {
		t.Fatalf("hidden = %v, want only the surviving provider's section", policy.HiddenSections)
	}
	if good.calls != 1 {
		t.Fatalf("the provider after the panicking one was consulted %d times, want 1", good.calls)
	}

	// A provider that never answers is cut off by the deadline rather than
	// hanging the caller. The context is cancelled here so the test does not
	// wait out the whole two seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stuck := &uiPolicyExt{baseExt: baseExt{id: "policy.stuck"}, block: true}
	done := make(chan UIPolicy, 1)
	go func() { done <- UIPolicyResolver(managerWith(t, stuck))(ctx) }()
	select {
	case got := <-done:
		if !got.Empty() {
			t.Fatalf("a provider that never answered contributed %+v", got)
		}
	case <-time.After(uiPolicyCallTimeout + time.Second):
		t.Fatal("the resolver did not return, the deadline is not being applied")
	}
}

// Hiding a section is not the only defence: the same paths reach the
// configuration lock check, so a write to a hidden path is refused even by a
// client that never rendered the section.
func TestUIPolicyRestrictsTheWritePath(t *testing.T) {
	config.ClearOverlayProviders()
	t.Cleanup(config.ClearOverlayProviders)
	t.Cleanup(func() { SetUIPolicyResolver(nil) })

	if config.IsKeyLocked("providerAccounts.anthropic.apiKey") {
		t.Fatal("the key is already locked, the test cannot tell what caused the refusal")
	}

	SetUIPolicyResolver(func(context.Context) UIPolicy {
		return UIPolicy{
			HiddenSections:   []string{"providerAccounts"},
			ReadOnlySections: []string{"tui.theme"},
		}
	})

	if !config.IsKeyLocked("providerAccounts.anthropic.apiKey") {
		t.Fatal("a key under a hidden section is not locked, hiding would be the only defence")
	}
	err := config.ErrIfLocked("providerAccounts.anthropic.apiKey")
	if !errors.Is(err, config.ErrKeyLocked) {
		t.Fatalf("write refusal = %v, want a locked-key error", err)
	}
	if !errors.Is(config.ErrIfLocked("tui.theme"), config.ErrKeyLocked) {
		t.Fatal("a read-only section is offered for writing")
	}
	if config.ErrIfLocked("tui.nerdFonts") != nil {
		t.Fatal("an unrestricted key was refused")
	}
	if got := config.LockedKeys(); !reflect.DeepEqual(got, []string{"providerAccounts", "tui.theme"}) {
		t.Fatalf("LockedKeys = %v, want the policy's paths", got)
	}

	// Removing the resolver restores the unextended behaviour exactly.
	SetUIPolicyResolver(nil)
	if config.IsKeyLocked("providerAccounts.anthropic.apiKey") || len(config.LockedKeys()) != 0 {
		t.Fatal("the restriction outlived the policy that imposed it")
	}
}

// The process-wide policy is memoised, so the lock check on a write path does
// not fan out to extensions once per changed key, and invalidating it makes the
// next read ask again.
func TestCurrentUIPolicyMemoisesAndInvalidates(t *testing.T) {
	t.Cleanup(func() { SetUIPolicyResolver(nil) })

	calls := 0
	SetUIPolicyResolver(func(context.Context) UIPolicy {
		calls++
		return UIPolicy{HiddenSections: []string{"mcpServers"}}
	})

	for range 5 {
		if !CurrentUIPolicy(context.Background()).IsHidden("mcpServers.github") {
			t.Fatal("the policy was not applied")
		}
	}
	if calls != 1 {
		t.Fatalf("providers asked %d times for five reads, want 1", calls)
	}

	InvalidateUIPolicy()
	CurrentUIPolicy(context.Background())
	if calls != 2 {
		t.Fatalf("providers asked %d times after invalidation, want 2", calls)
	}
}
