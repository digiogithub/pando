package agent

import (
	"context"
	"errors"
	"testing"
)

func TestResumeRegistryNilAndEmptyTakeNothing(t *testing.T) {
	var nilReg *ResumeRegistry
	if taken, err := nilReg.Offer("s", nil); taken || err != nil {
		t.Fatalf("nil registry: taken=%v err=%v", taken, err)
	}
	if taken, err := NewResumeRegistry().Offer("s", nil); taken || err != nil {
		t.Fatalf("empty registry: taken=%v err=%v", taken, err)
	}
	// Registering on a nil registry must not panic.
	nilReg.Register(ResumePriorityOwner, func(string, ResumeStart) (bool, error) { return true, nil })()
}

func TestResumeRegistryPriorityOrderAndDecline(t *testing.T) {
	r := NewResumeRegistry()
	var order []string
	r.Register(ResumePriorityFallback, func(string, ResumeStart) (bool, error) {
		order = append(order, "fallback")
		return true, nil
	})
	r.Register(ResumePriorityOwner, func(sessionID string, _ ResumeStart) (bool, error) {
		order = append(order, "owner")
		return sessionID == "mine", nil
	})

	if taken, _ := r.Offer("mine", nil); !taken {
		t.Fatal("expected a handler to take it")
	}
	if len(order) != 1 || order[0] != "owner" {
		t.Fatalf("owner must win its own session, order=%v", order)
	}

	order = nil
	if taken, _ := r.Offer("other", nil); !taken {
		t.Fatal("expected the fallback to take it")
	}
	if len(order) != 2 || order[0] != "owner" || order[1] != "fallback" {
		t.Fatalf("declined owner must hand over to the fallback, order=%v", order)
	}
}

func TestResumeRegistryUnregisterAndError(t *testing.T) {
	r := NewResumeRegistry()
	unreg := r.Register(ResumePriorityFallback, func(string, ResumeStart) (bool, error) {
		return true, ErrSessionBusy
	})
	if taken, err := r.Offer("s", nil); !taken || !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("taken=%v err=%v", taken, err)
	}
	unreg()
	unreg() // idempotent
	if taken, _ := r.Offer("s", nil); taken {
		t.Fatal("unregistered handler must not be offered runs")
	}
}

func TestResumeRegistryPassesStart(t *testing.T) {
	r := NewResumeRegistry()
	called := false
	r.Register(ResumePriorityFallback, func(_ string, start ResumeStart) (bool, error) {
		_, err := start(context.Background())
		return true, err
	})
	want := errors.New("boom")
	taken, err := r.Offer("s", func(context.Context) (<-chan AgentEvent, error) {
		called = true
		return nil, want
	})
	if !taken || !called || !errors.Is(err, want) {
		t.Fatalf("taken=%v called=%v err=%v", taken, called, err)
	}
}
