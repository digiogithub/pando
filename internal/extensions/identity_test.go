package extensions

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/pkg/extension"
)

type identityExt struct {
	baseExt
	id     extension.Identity
	known  bool
	panics bool
	block  bool

	calls int
}

func (e *identityExt) ExtensionInfo() extension.Info { return e.info(e) }

func (e *identityExt) Identity(ctx context.Context) (extension.Identity, bool) {
	e.calls++
	if e.panics {
		panic("boom")
	}
	if e.block {
		<-ctx.Done()
		return extension.Identity{}, false
	}
	return e.id, e.known
}

// A build with no identity provider must behave exactly as it did before the
// capability existed: the resolver is usable and always says "I do not know".
func TestIdentityResolverWithoutProvider(t *testing.T) {
	resolve := IdentityResolver(managerWith(t))
	if resolve == nil {
		t.Fatal("resolver is nil, callers would have to nil-check it")
	}
	if id, ok := resolve(context.Background()); ok || id.UserID != "" || len(id.Groups) != 0 {
		t.Fatalf("got identity %+v, ok=%v with no provider loaded", id, ok)
	}
}

func TestIdentityResolverNilManager(t *testing.T) {
	if _, ok := IdentityResolver(nil)(context.Background()); ok {
		t.Fatal("a nil manager reported an identity")
	}
}

// The first provider that knows wins, and the ones after it are not consulted.
func TestIdentityFirstKnownProviderWins(t *testing.T) {
	first := &identityExt{baseExt: baseExt{id: "identity.a"}}
	second := &identityExt{
		baseExt: baseExt{id: "identity.b"},
		id:      extension.Identity{UserID: "u-2", Email: "b@example.test", Groups: []string{"eng"}},
		known:   true,
	}
	third := &identityExt{
		baseExt: baseExt{id: "identity.c"},
		id:      extension.Identity{UserID: "u-3"},
		known:   true,
	}

	resolve := IdentityResolver(managerWith(t, first, second, third))
	id, ok := resolve(context.Background())
	if !ok {
		t.Fatal("no identity reported")
	}
	if id.UserID != "u-2" || id.Email != "b@example.test" {
		t.Fatalf("wrong provider answered: %+v", id)
	}
	if len(id.Groups) != 1 || id.Groups[0] != "eng" {
		t.Fatalf("groups not carried: %+v", id.Groups)
	}
	if third.calls != 0 {
		t.Fatalf("a provider after the first answer was consulted %d times", third.calls)
	}
}

// A panicking provider must be contained and must not stop the ones after it
// from answering: a memory write is on the other side of this call.
func TestIdentityPanicContained(t *testing.T) {
	bad := &identityExt{baseExt: baseExt{id: "identity.bad"}, panics: true}
	good := &identityExt{
		baseExt: baseExt{id: "identity.good"},
		id:      extension.Identity{UserID: "u-1"},
		known:   true,
	}

	id, ok := IdentityResolver(managerWith(t, bad, good))(context.Background())
	if !ok || id.UserID != "u-1" {
		t.Fatalf("a panicking provider cost the next one its answer: %+v ok=%v", id, ok)
	}
}

// The resolver is read per use, so a provider that signs in between two calls
// is seen without a restart.
func TestIdentityReadPerUse(t *testing.T) {
	ext := &identityExt{baseExt: baseExt{id: "identity.late"}}
	resolve := IdentityResolver(managerWith(t, ext))

	if _, ok := resolve(context.Background()); ok {
		t.Fatal("identity reported before sign-in")
	}
	ext.id, ext.known = extension.Identity{UserID: "u-9"}, true
	id, ok := resolve(context.Background())
	if !ok || id.UserID != "u-9" {
		t.Fatalf("sign-in not picked up: %+v ok=%v", id, ok)
	}
}

// A wedged provider is cut off rather than allowed to hold up the write path.
func TestIdentityProviderTimeout(t *testing.T) {
	blocked := &identityExt{baseExt: baseExt{id: "identity.slow"}, block: true}
	resolve := IdentityResolver(managerWith(t, blocked))

	// A caller deadline shorter than identityCallTimeout keeps the test fast
	// while exercising the same cancellation path.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := resolve(ctx); ok {
			t.Error("a provider that never answered reported an identity")
		}
	}()

	select {
	case <-done:
	case <-time.After(identityCallTimeout + time.Second):
		t.Fatal("resolver did not return after the provider deadline")
	}
}

// The host copies the group list rather than keeping the extension's slice.
func TestIdentityGroupsAreCopied(t *testing.T) {
	groups := []string{"eng"}
	ext := &identityExt{
		baseExt: baseExt{id: "identity.groups"},
		id:      extension.Identity{UserID: "u-1", Groups: groups},
		known:   true,
	}
	id, _ := IdentityResolver(managerWith(t, ext))(context.Background())
	groups[0] = "mutated"
	if id.Groups[0] != "eng" {
		t.Fatal("the host kept the extension's slice instead of a copy")
	}
}
