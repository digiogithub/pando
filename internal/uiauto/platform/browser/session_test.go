package browser

import (
	"context"
	"testing"
)

func TestRegisterUnregisterActiveSession(t *testing.T) {
	if _, ok := ActiveSession(); ok {
		t.Fatal("precondition: no session should be registered yet")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RegisterSession("s1", ctx)
	t.Cleanup(func() { UnregisterSession("s1") })

	got, ok := ActiveSession()
	if !ok || got != ctx {
		t.Fatalf("ActiveSession() = %v, %v", got, ok)
	}
}

func TestActiveSessionCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	RegisterSession("s2", ctx)
	t.Cleanup(func() { UnregisterSession("s2") })
	cancel()

	if _, ok := ActiveSession(); ok {
		t.Fatal("expected ActiveSession to report unavailable once its context is canceled")
	}
}

func TestUnregisterSessionOnlyMatchingID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	RegisterSession("s3", ctx)
	t.Cleanup(func() { UnregisterSession("s3") })

	// Unregistering a different (stale) id must not clear the current one.
	UnregisterSession("some-other-id")
	if _, ok := ActiveSession(); !ok {
		t.Fatal("expected the active session to remain registered")
	}
}
