package extensions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGuardValueContainsPanic(t *testing.T) {
	v, ok := guardValue("boom", "tools.demo", func() int { panic("no") })
	if ok || v != 0 {
		t.Fatalf("guardValue = (%v, %v), want (0, false)", v, ok)
	}

	v, ok = guardValue("fine", "tools.demo", func() int { return 7 })
	if !ok || v != 7 {
		t.Fatalf("guardValue = (%v, %v), want (7, true)", v, ok)
	}
}

func TestGuardErrTurnsPanicIntoError(t *testing.T) {
	err := guardErr("run", "tools.demo", func() error { panic("no") })
	if err == nil || !strings.Contains(err.Error(), "tools.demo") {
		t.Fatalf("err = %v, want an error naming the extension", err)
	}

	want := errors.New("failed")
	if got := guardErr("run", "tools.demo", func() error { return want }); !errors.Is(got, want) {
		t.Fatalf("err = %v, want the returned error passed through", got)
	}
}

func TestGuardDeclarativeReleasesTheCallerOnAHang(t *testing.T) {
	// A wedged extension cannot be killed, so the contract is only that the
	// caller is released. Shorten the deadline for the test rather than
	// waiting 30s for it.
	restore := declarativeTimeoutForTest(50 * time.Millisecond)
	defer restore()

	blocked := make(chan struct{})
	defer close(blocked)

	start := time.Now()
	_, ok := guardDeclarative(context.Background(), "hang", "tools.demo", func(context.Context) int {
		<-blocked
		return 1
	})
	if ok {
		t.Fatal("a hung call reported success")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("caller was held for %s, want release at the deadline", elapsed)
	}
}

func TestGuardDeclarativePassesValuesThrough(t *testing.T) {
	got, ok := guardDeclarative(context.Background(), "list", "tools.demo",
		func(context.Context) []string { return []string{"a", "b"} })
	if !ok || len(got) != 2 {
		t.Fatalf("guardDeclarative = (%v, %v), want the value through", got, ok)
	}
}

func TestGuardDeclarativeContainsPanic(t *testing.T) {
	_, ok := guardDeclarative(context.Background(), "list", "tools.demo",
		func(context.Context) []string { panic("no") })
	if ok {
		t.Fatal("a panicking call reported success")
	}
}
